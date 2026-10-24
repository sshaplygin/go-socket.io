'use strict';

const assert = require('assert');
const io = require('socket.io-client');
const version = require('socket.io-client/package.json').version;
assert(['1.7.4', '2.5.0'].includes(version), 'unexpected client version: ' + version);
if (process.env.CONFORMANCE_CLIENT_VERSION) {
  assert.strictEqual(version, process.env.CONFORMANCE_CLIENT_VERSION);
}

const base = process.argv[2];
const transport = process.argv[3];
const sockets = [];
const deadline = setTimeout(() => {
  console.error('conformance scenario timed out');
  process.exit(1);
}, 30000);

function event(socket, name) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      socket.removeListener(name, done);
      reject(new Error('event timed out: ' + name));
    }, 5000);
    function done(...args) { clearTimeout(timer); resolve(args); }
    socket.once(name, done);
  });
}

function ack(socket, name, ...args) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('ACK timed out: ' + name)), 5000);
    socket.emit(name, ...args, (...values) => { clearTimeout(timer); resolve(values); });
  });
}

async function connect(options = {}, onEngine) {
  const socket = io(base, Object.assign({
    autoConnect: false, forceNew: true, forceNode: true,
    transports: transport === 'upgrade' ? ['polling', 'websocket'] : [transport],
    reconnection: false, timeout: 5000
  }, options));
  sockets.push(socket);
  const connected = event(socket, 'connect');
  socket.connect();
  if (onEngine) { onEngine(socket.io.engine); }
  await connected;
  return socket;
}

function bytes(value) {
  assert(Buffer.isBuffer(value), 'expected a binary Buffer, got ' + JSON.stringify(value));
  return Array.from(value);
}

async function basic() {
  const root = await connect();
  assert.strictEqual(root.io.engine.transport.name, transport);
  assert.deepStrictEqual(await ack(root, 'echo', 'hello €👍 你好'), ['hello €👍 你好']);

  const chat = root.io.socket('/chat');
  sockets.push(chat);
  const chatConnected = event(chat, 'connect');
  chat.connect();
  await chatConnected;
  assert.deepStrictEqual(await ack(chat, 'echo', 'namespace'), ['namespace']);

  for (const [name, reason] of [['/missing', 'Invalid namespace'], ['/denied', 'access denied']]) {
    const rejected = root.io.socket(name);
    sockets.push(rejected);
    const rejection = event(rejected, 'error');
    rejected.connect();
    const [error] = await rejection;
    assert.strictEqual(error, reason);
    assert.strictEqual(root.connected, true);
    assert.deepStrictEqual(await ack(root, 'echo', 'still connected'), ['still connected']);
    rejected.disconnect();
  }

  root.once('question', (value, reply) => {
    assert.strictEqual(value, 'server');
    reply('client ACK');
  });
  const serverAck = event(root, 'ack-result');
  await ack(root, 'ask');
  assert.deepStrictEqual(await serverAck, ['client ACK']);

  const payload = Buffer.from([0, 1, 127, 128, 255]);
  const binaryEvent = event(root, 'binary-event');
  const binaryAck = await ack(root, 'binary', payload);
  assert.deepStrictEqual(bytes((await binaryEvent)[0]), Array.from(payload));
  assert.deepStrictEqual(bytes(binaryAck[0]), Array.from(payload));
  root.once('binary-question', (value, reply) => {
    assert.deepStrictEqual(bytes(value), [0, 255, 2]);
    reply(Buffer.from([255, 0, 3]));
  });
  const binaryServerAck = event(root, 'binary-ack-result');
  await ack(root, 'ask-binary');
  assert.deepStrictEqual(bytes((await binaryServerAck)[0]), [255, 0, 3]);

  const peer = await connect();
  const outsider = await connect();
  assert.deepStrictEqual(await ack(root, 'join', 'room'), ['joined']);
  assert.deepStrictEqual(await ack(peer, 'join', 'room'), ['joined']);
  const received = [[], [], []];
  [root, peer, outsider].forEach((s, i) => s.on('news', value => received[i].push(value)));
  const barriers = [root, peer, outsider].map(s => event(s, 'barrier'));
  assert.deepStrictEqual(await ack(root, 'broadcast', 'room'), ['sent']);
  await Promise.all(barriers);
  assert.deepStrictEqual(received, [[], ['room'], []]);

  const disconnected = event(chat, 'disconnect');
  chat.emit('disconnect-me');
  assert.deepStrictEqual(await disconnected, ['io server disconnect']);
  assert.strictEqual(root.connected, true);
  assert.deepStrictEqual(await ack(root, 'echo', 'after disconnect'), ['after disconnect']);

  const localDisconnect = event(root, 'disconnect');
  root.disconnect();
  assert.deepStrictEqual(await localDisconnect, ['io client disconnect']);
  console.log(version + ' ' + transport + ': root/namespace, rejection, ACK, binary, Unicode, rooms and disconnect passed');
}

async function reconnect() {
  const root = await connect({reconnection: true, reconnectionDelay: 20, reconnectionDelayMax: 20, randomizationFactor: 0, reconnectionAttempts: 3});
  const before = (await ack(root, 'id'))[0];
  const disconnected = event(root, 'disconnect');
  const reconnected = event(root, 'connect');
  root.emit('drop-transport');
  const [reason] = await disconnected;
  assert.notStrictEqual(reason, 'io server disconnect');
  await reconnected;
  assert.notStrictEqual((await ack(root, 'id'))[0], before);
  assert.deepStrictEqual(await ack(root, 'echo', 'reconnected'), ['reconnected']);
  root.disconnect();
  console.log(version + ' ' + transport + ': reconnect passed');
}

async function upgrade() {
  let upgraded;
  const root = await connect({}, engine => {
    assert.strictEqual(engine.transport.name, 'polling');
    upgraded = event(engine, 'upgrade');
  });
  const engine = root.io.engine;
  await upgraded;
  assert.strictEqual(engine.transport.name, 'websocket');
  assert.deepStrictEqual(await ack(root, 'echo', 'upgraded'), ['upgraded']);
  root.disconnect();
  console.log(version + ': polling-to-websocket upgrade passed');
}

async function base64Binary() {
  let upgraded;
  const root = await connect({forceBase64: true}, engine => {
    if (transport === 'upgrade') { upgraded = event(engine, 'upgrade'); }
  });
  if (upgraded) { await upgraded; }
  const engine = root.io.engine;
  assert.strictEqual(engine.transport.name, 'websocket');
  // Base64 mode carries binary packets in text messages in both directions.
  const frames = [];
  engine.transport.ws.on('message', data => frames.push(typeof data));
  const received = event(root, 'binary-event');
  const payload = Buffer.from([1, 2, 255]);
  const reply = await ack(root, 'binary', payload);
  assert.deepStrictEqual(bytes((await received)[0]), Array.from(payload));
  assert.deepStrictEqual(bytes(reply[0]), Array.from(payload));
  assert(frames.length >= 4, 'expected binary event and ACK messages, got ' + frames.length);
  assert(frames.every(type => type === 'string'), 'server sent a binary websocket message: ' + frames);
  root.disconnect();
  console.log(version + ' ' + transport + ': forceBase64 binary exchange passed');
}

(async () => {
  try {
    if (transport === 'upgrade') { await upgrade(); }
    else { await basic(); await reconnect(); }
    if (transport === 'websocket' || transport === 'upgrade') { await base64Binary(); }
  } finally {
    sockets.forEach(socket => socket.disconnect());
    clearTimeout(deadline);
  }
})().catch(error => { console.error(error); process.exit(1); });
