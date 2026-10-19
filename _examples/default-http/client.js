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
async function connect(transport, url = base) {
  const socket = io(url, {autoConnect: false, forceNew: true, forceNode: true,
    transports: [transport], reconnection: false, timeout: 5000});
  sockets.push(socket);
  socket.on('connect_error', finish);
  socket.on('error', finish);
  const connected = event(socket, 'connect');
  socket.connect(); await connected;
  return socket;
}
(async () => {
  // One peer uses polling, the other websocket, so the room crosses transports.
  const alice = await connect('polling');
  let login = event(alice, 'login');
  assert.deepStrictEqual(await ack(alice, 'add user', 'alice'), []);
  assert.strictEqual((await login)[0].numUsers, 1);
  const joined = event(alice, 'user joined');
  const bob = await connect('websocket', process.argv[3] || base);
  login = event(bob, 'login');
  await ack(bob, 'add user', 'bob');
  assert.strictEqual((await login)[0].numUsers, 2);
  assert.deepStrictEqual(await joined, [{username: 'bob', numUsers: 2}]);
  let aliceEchoes = 0, bobEchoes = 0;
  alice.on('new message', () => aliceEchoes++);
  bob.on('typing', () => bobEchoes++);
  const message = event(bob, 'new message');
  await ack(alice, 'new message', 'hello room');
  assert.deepStrictEqual(await message, [{username: 'alice', message: 'hello room'}]);
  assert.strictEqual(aliceEchoes, 0); // The ACK follows any incorrectly queued sender copy.
  const typing = event(alice, 'typing');
  await ack(bob, 'typing');
  assert.deepStrictEqual(await typing, [{username: 'bob'}]);
  assert.strictEqual(bobEchoes, 0);
  const left = event(alice, 'user left');
  bob.close();
  assert.deepStrictEqual(await left, [{username: 'bob', numUsers: 1}]);
  console.log('chat example passed with socket.io-client ' + version);
})().then(() => finish(), finish);
