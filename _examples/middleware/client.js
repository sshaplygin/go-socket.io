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
function manager(transport, token) {
  return new io.Manager(base, {autoConnect: false, forceNode: true, transports: [transport],
    query: {token, name: 'alice'}, reconnection: false, timeout: 5000});
}
function socket(manager, name) {
  const s = manager.socket(name);
  sockets.push(s);
  s.on('connect_error', finish);
  return s;
}
async function scenario(transport) {
  const deniedManager = manager(transport, 'wrong');
  const root = socket(deniedManager, '/'), denied = socket(deniedManager, '/secure');
  root.on('error', finish);
  denied.once('connect', () => finish(new Error('namespace middleware did not reject')));
  const rootReady = event(root, 'connect'), rejection = event(denied, 'error');
  root.connect(); denied.connect();
  await rootReady;
  assert.deepStrictEqual(await rejection, ['unauthorized']);
  assert.deepStrictEqual(await ack(root, 'health'), ['pong']); // Rejection keeps other namespaces usable.
  const secure = socket(manager(transport, 'demo-secret'), '/secure');
  const ready = event(secure, 'connect');
  secure.connect(); await ready;
  assert.deepStrictEqual(await ack(secure, 'echo', 'hello'), ['alice: hello']);
  let blockedAck = false;
  const blocked = event(secure, 'error');
  secure.emit('blocked', 'value', () => { blockedAck = true; });
  assert.deepStrictEqual(await blocked, ['event blocked']);
  assert.deepStrictEqual(await ack(secure, 'echo', 'still open'), ['alice: still open']);
  assert.strictEqual(blockedAck, false); // The later ACK is a wire barrier for the rejected handler.
  for (const s of [root, denied, secure]) s.close();
}
(async () => {
  for (const transport of ['polling', 'websocket']) await scenario(transport);
  console.log('middleware example passed with socket.io-client ' + version);
})().then(() => finish(), finish);
