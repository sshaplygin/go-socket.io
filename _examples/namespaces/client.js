'use strict';
const assert = require('assert');
const io = require('socket.io-client');
const version = require('socket.io-client/package.json').version;
assert(['1.7.4', '2.5.0'].includes(version));
const base = process.argv[2];
assert(base, 'usage: node client.js <server URL>');
const sockets = [];
let finished = false;
const deadline = setTimeout(() => finish(new Error('example timed out')), 10000);
function finish(error) {
  if (finished) return;
  finished = true;
  clearTimeout(deadline);
  for (const socket of sockets) socket.close();
  if (error) { console.error(error); process.exit(1); }
  process.stdout.write('', () => process.exit(0));
}
function event(socket, name) {
  return new Promise(resolve => socket.once(name, (...args) => resolve(args)));
}
function ack(socket, name, ...args) {
  return new Promise(resolve => socket.emit(name, ...args, (...values) => resolve(values)));
}
function manager(transport, name, token = 'demo-secret') {
  return new io.Manager(base, {autoConnect: false, forceNode: true, transports: [transport],
    query: {token, name}, reconnection: false, timeout: 5000});
}
function socket(manager, namespace) {
  const s = manager.socket(namespace);
  sockets.push(s);
  s.on('connect_error', finish);
  return s;
}
async function connect(...list) {
  const ready = list.map(s => event(s, 'connect'));
  for (const s of list) s.connect();
  await Promise.all(ready);
}
async function scenario(transport) {
  const aliceManager = manager(transport, 'alice');
  const alice = socket(aliceManager, '/chat');
  const news = socket(aliceManager, '/news');
  const bob = socket(manager(transport, 'bob'), '/chat');
  const outsider = socket(manager(transport, 'outsider'), '/chat');
  for (const s of [alice, news, bob, outsider]) s.on('error', finish);
  await connect(alice, news, bob, outsider);
  assert.strictEqual(alice.io.engine.id, news.io.engine.id);
  // The 1.7.4 Manager exposes the engine ID on every namespace socket.
  const legacyID = version === '1.7.4';
  assert.strictEqual(alice.id, (legacyID ? '' : '/chat#') + alice.io.engine.id);
  assert.strictEqual(news.id, (legacyID ? '' : '/news#') + news.io.engine.id);
  assert.deepStrictEqual(await ack(alice, 'join', 'red'), ['red']);
  assert.deepStrictEqual(await ack(bob, 'join', 'red'), ['red']);
  assert.deepStrictEqual(await ack(outsider, 'join', 'blue'), ['blue']);
  const meta = (await ack(alice, 'inspect'))[0];
  assert.strictEqual(meta.namespace, '/chat');
  assert.strictEqual(meta.id, '/chat#' + alice.io.engine.id);
  assert(meta.rooms.includes('red'));
  let senderMessages = 0, outsiderMessages = 0, newsMessages = 0;
  alice.on('message', () => senderMessages++);
  outsider.on('message', () => outsiderMessages++);
  news.on('message', () => newsMessages++);
  const delivered = event(bob, 'message');
  assert.deepStrictEqual(await ack(alice, 'say', 'red', 'hello €👍'), ['sent']);
  assert.deepStrictEqual(await delivered, ['alice', 'hello €👍']);
  assert.strictEqual(senderMessages, 0); // ACK follows any wrongly queued sender copy.
  await ack(outsider, 'inspect');
  assert.strictEqual(outsiderMessages, 0); // Same-connection wire barrier after the broadcast.
  assert.deepStrictEqual(await ack(news, 'echo', 'still separate'), ['news: still separate']);
  assert.strictEqual(newsMessages, 0);
  const denied = socket(manager(transport, 'intruder', 'wrong'), '/chat');
  denied.once('connect', () => finish(new Error('unauthorized namespace connected')));
  const rejection = event(denied, 'error');
  denied.connect();
  assert.deepStrictEqual(await rejection, ['unauthorized']);
  denied.close();
  for (const s of [alice, news, bob, outsider]) s.close();
}
(async () => {
  for (const transport of ['polling', 'websocket']) await scenario(transport);
  console.log('namespace example passed with socket.io-client ' + version);
})().then(() => finish(), finish);
