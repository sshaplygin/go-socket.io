'use strict';
const assert = require('assert');
const crypto = require('crypto');
const http = require('http');
const io = require('socket.io-client');
const version = require('socket.io-client/package.json').version;
assert(['1.7.4', '2.5.0'].includes(version));
if (process.env.CONFORMANCE_CLIENT_VERSION) assert.strictEqual(version, process.env.CONFORMANCE_CLIENT_VERSION);
const [base, scenario, transport] = process.argv.slice(2);
const sockets = [];
const transcript = [];
const sequence = new Map();
const deadline = setTimeout(() => {
  console.error('differential scenario timed out: ' + JSON.stringify(transcript));
  process.exit(1);
}, 10000);
function record(socket, event, args) {
  const index = socket === null ? -1 : sockets.indexOf(socket);
  const seq = sequence.get(index) || 0;
  sequence.set(index, seq + 1);
  transcript.push({socket: index, seq, event, args});
}
function event(socket, name) {
  return new Promise(resolve => socket.once(name, (...args) => resolve(args)));
}
function ack(socket, name, ...args) {
  return new Promise(resolve => socket.emit(name, ...args, (...values) => {
    record(socket, 'ack:' + name, values.map(observable));
    resolve(values);
  }));
}
function track(socket) {
  sockets.push(socket);
  // Capture every incoming application event, including unexpected ones. Hook the
  // decoded packet before Socket adds its local ACK callback to the arguments.
  const onEvent = socket.onevent;
  socket.onevent = function(packet) {
    record(socket, packet.data[0], packet.data.slice(1).map(observable));
    return onEvent.call(this, packet);
  };
  socket.on('connect', () => record(socket, 'connect', [{id: socket.id, sid: socket.io.engine.id, namespace: socket.nsp}]));
  socket.on('disconnect', reason => record(socket, 'disconnect', [reason]));
  socket.on('error', error => record(socket, 'error', [error]));
  socket.on('connect_error', error => { console.error(error); process.exit(1); });
  return socket;
}
async function connect(options = {}) {
  const socket = track(io(base, Object.assign({autoConnect: false, forceNew: true,
    forceNode: true, transports: [transport], reconnection: false, timeout: 5000}, options)));
  const connected = event(socket, 'connect');
  socket.connect();
  await connected;
  return socket;
}
async function namespace(root, name) {
  const socket = track(root.io.socket(name));
  const ready = event(socket, name === '/missing' || name === '/denied' ? 'error' : 'connect');
  socket.connect();
  await ready;
  return socket;
}
// Values longer than this are recorded by length and SHA-256, not in full.
const summarized = 256;
function digest(value) {
  return crypto.createHash('sha256').update(value).digest('hex');
}
function binary(value) {
  assert(Buffer.isBuffer(value), 'expected binary Buffer');
  if (value.length > summarized) return {$binary: value.length, sha256: digest(value)};
  return {$binary: Array.from(value)};
}
function observable(value) {
  if (Buffer.isBuffer(value)) return binary(value);
  if (typeof value === 'string' && value.length > summarized) return {$string: value.length, sha256: digest(value)};
  if (Array.isArray(value)) return value.map(observable);
  if (value && typeof value === 'object') {
    const result = {};
    for (const key of Object.keys(value)) result[key] = observable(value[key]);
    return result;
  }
  return value;
}
function request(method, path, headers = {}) {
  return new Promise((resolve, reject) => {
    const req = http.request(base + path, {method, headers}, response => {
      const chunks = [];
      response.on('data', chunk => chunks.push(chunk));
      response.on('end', () => resolve({status: response.statusCode, headers: response.headers,
        body: Buffer.concat(chunks).toString('utf8')}));
    });
    req.on('error', reject);
    req.end();
  });
}
async function raw() {
  const path = '/socket.io/?EIO=3&transport=' + transport;
  if (scenario === 'rejected-handshake') {
    for (const [method, suffix] of [['POST', ''], ['GET', '&sid=unknown'], ['GET', '&transport=invalid']]) {
      // Replace the transport for the last request; duplicate query keys differ across parsers.
      const url = suffix.includes('transport=') ? '/socket.io/?EIO=3&transport=invalid' : path + suffix;
      const response = await request(method, url);
      record(null, method + suffix, [response.status, JSON.parse(response.body)]);
    }
  } else if (scenario === 'cors') {
    for (const headers of [{}, {Origin: 'https://differential.example'}]) {
      const response = await request('GET', '/socket.io/?EIO=3&transport=polling&b64=1', headers);
      const cors = {};
      for (const key of Object.keys(response.headers).sort()) {
        if (key.startsWith('access-control-')) cors[key] = response.headers[key];
      }
      record(null, 'cors', [response.status, cors]);
    }
  } else {
    const engine = require('engine.io-client')(base, {path: '/socket.io/', transports: [transport]});
    const [open] = await event(engine, 'handshake');
    assert.strictEqual(engine.transport.name, transport);
    record(null, 'open', [{sid: open.sid, pingInterval: open.pingInterval, pingTimeout: open.pingTimeout}]);
    engine.close();
  }
}
async function run() {
  if (['rejected-handshake', 'cors', 'open-packet'].includes(scenario)) { await raw(); return; }
  const root = await connect(scenario === 'handshake-data' ?
    {query: {custom: 'query-value'}, extraHeaders: {'X-Differential': 'header-value'}} : {});
  if (scenario === 'connect-root') {
    await ack(root, 'identity');
  } else if (scenario === 'namespace') {
    const chat = await namespace(root, '/chat');
    await ack(chat, 'identity');
    await ack(chat, 'echo', 'namespace');
    const missing = await namespace(root, '/missing');
    assert(root.connected);
    await ack(root, 'echo', 'root remains');
    missing.disconnect();
    chat.disconnect();
  } else if (scenario === 'middleware-reject') {
    const denied = await namespace(root, '/denied');
    assert(root.connected);
    await ack(root, 'echo', 'after rejection');
    denied.disconnect();
  } else if (scenario === 'ack-both-ways') {
    root.once('question', (value, reply) => reply('client ACK'));
    const received = event(root, 'ack-result');
    await ack(root, 'echo', 'client');
    await ack(root, 'ask');
    await received;
  } else if (scenario === 'binary') {
    const echoed = event(root, 'binary-event');
    // Binary ACKs need conversion before recording; unlike JSON byte arrays they remain Buffers.
    const value = Buffer.from([0, 1, 127, 128, 255]);
    const replies = await ack(root, 'binary', value);
    binary(replies[0]);
    await echoed;
    root.once('binary-question', (question, reply) => {
      binary(question); reply(Buffer.from([255, 0, 3]));
    });
    const result = event(root, 'binary-result');
    await ack(root, 'ask-binary');
    await result;
  } else if (scenario === 'non-ascii') {
    await ack(root, 'echo', 'hello €👍 你好');
  } else if (scenario === 'rooms') {
    const peer = await connect();
    const outsider = await connect();
    await ack(root, 'join', 'room');
    await ack(peer, 'join', 'room');
    for (const except of [false, true]) {
      const barriers = [root, peer, outsider].map(socket => event(socket, 'barrier'));
      await ack(root, 'room', 'room', except);
      await Promise.all(barriers);
    }
    await ack(root, 'leave', 'room');
    const barriers = [root, peer, outsider].map(socket => event(socket, 'barrier'));
    await ack(root, 'room', 'room', false);
    await Promise.all(barriers);
    peer.disconnect(); outsider.disconnect();
  } else if (scenario === 'server-disconnect') {
    for (const name of ['disconnect-one', 'disconnect-all', 'drop-transport']) {
      const socket = name === 'disconnect-one' ? root : await connect();
      const disconnected = event(socket, 'disconnect');
      socket.emit(name);
      await disconnected;
    }
  } else if (scenario === 'connect-broadcast') {
    // The connection handlers of joiner and its /welcome socket broadcast while
    // they connect; root is a peer already in the root namespace. Each ACK follows
    // the packets queued before it on that socket.
    const joiner = await connect({query: {welcome: '1'}});
    const welcome = await namespace(joiner, '/welcome');
    for (const socket of [joiner, welcome, root]) await ack(socket, 'echo', 'barrier');
    welcome.disconnect();
    joiner.disconnect();
  } else if (scenario === 'handshake-data') {
    // Preserve raw timestamps for the Go comparison's validation/normalization.
    await new Promise(resolve => root.emit('metadata', (...values) => {
      const value = values[0];
      assert(Number.isFinite(Date.parse(value.time)), 'invalid handshake time');
      value.time = new Date(value.time).toISOString();
      record(root, 'ack:metadata', values.map(observable)); resolve();
    }));
  } else if (scenario === 'large-payload') {
    // Close to the default message limit of 1e6 bytes, in both directions.
    await ack(root, 'echo', 'x'.repeat(999000));
    const echoed = event(root, 'binary-event');
    await ack(root, 'binary', Buffer.from(Array.from({length: 600000}, (_, i) => i % 251)));
    await echoed;
  } else if (scenario === 'over-limit') {
    // The EVENT packet is 12 bytes longer than the limit.
    const disconnected = event(root, 'disconnect');
    root.emit('echo', 'x'.repeat(1e6));
    await disconnected;
  } else if (scenario === 'burst') {
    // 300 packets of 1,000 characters exceed a polling response's 64 packets and 256 KiB.
    await ack(root, 'burst', 300, 1000);
  } else if (scenario === 'client-burst') {
    // Emitted at once, so a polling client sends them in one POST.
    for (let i = 0; i < 200; i++) root.emit('count');
    await ack(root, 'counted');
  } else if (scenario === 'attachments') {
    // 10 attachments, the parsers' limit, in an EVENT, an ACK and both directions.
    const values = Array.from({length: 10}, (_, i) => Buffer.from([i, 255 - i]));
    const echoed = event(root, 'binary-many-event');
    await ack(root, 'binary-many', values);
    await echoed;
    root.once('binary-many-question', (question, reply) => reply(question.map(value => Buffer.from(value).reverse())));
    const result = event(root, 'binary-many-result');
    await ack(root, 'ask-binary-many');
    await result;
  } else if (scenario === 'buffered-replay') {
    // Emitted between the drop of the transport and the reconnect: the client buffers
    // the packets and sends them to the new session.
    const replay = track(io(base, {autoConnect: false, forceNew: true, forceNode: true,
      transports: [transport], reconnection: true, reconnectionDelay: 20, reconnectionDelayMax: 20,
      randomizationFactor: 0, reconnectionAttempts: 3, timeout: 5000}));
    let connected = event(replay, 'connect');
    replay.connect();
    await connected;
    const disconnected = event(replay, 'disconnect');
    connected = event(replay, 'connect');
    replay.emit('drop-transport');
    await disconnected;
    assert(!replay.connected);
    const replies = [ack(replay, 'echo', 'buffered 1'), ack(replay, 'echo', 'buffered 2')];
    await connected;
    await Promise.all(replies);
    replay.disconnect();
  } else if (scenario === 'too-many-attachments') {
    const disconnected = event(root, 'disconnect');
    root.emit('binary-many', Array.from({length: 11}, (_, i) => Buffer.from([i])));
    await disconnected;
  } else {
    throw new Error('unknown scenario: ' + scenario);
  }
  if (root.connected) root.disconnect();
}
run().then(() => {
  for (const socket of sockets) socket.disconnect();
  transcript.sort((a, b) => a.socket - b.socket || a.seq - b.seq);
  clearTimeout(deadline);
  // A peer that rejected an in-flight POST can leave a drain pending even after
  // disconnect. The transcript is complete; do not let fixture cleanup hide it.
  process.stdout.write(JSON.stringify(transcript) + '\n', () => process.exit(0));
}, error => { console.error(error); process.exit(1); });
