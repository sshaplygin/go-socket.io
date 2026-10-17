'use strict';
const assert = require('assert');
const io = require('socket.io-client');
const version = require('socket.io-client/package.json').version;
assert(['1.7.4', '2.5.0'].includes(version), 'expected socket.io-client 1.7.4 or 2.5.0');
const base = process.argv[2];
assert(base, 'usage: node client.js <server URL>');
const sockets = [];
const deadline = setTimeout(() => finish(new Error('example timed out')), 10000);
function finish(error) {
  clearTimeout(deadline);
  for (const socket of sockets) socket.close();
  // A stalled polling POST can prevent close from draining; errors must end this runner.
  if (error) { console.error(error); process.exit(1); }
}
function event(socket, name) {
  return new Promise(resolve => socket.once(name, (...args) => resolve(args)));
}
function ack(socket, name, ...args) {
  return new Promise(resolve => socket.emit(name, ...args, (...values) => resolve(values)));
}
async function scenario(transport) {
  const socket = io(base, {autoConnect: false, forceNew: true, forceNode: true,
    transports: [transport], reconnection: false, timeout: 5000});
  sockets.push(socket);
  socket.on('connect_error', finish);
  const connected = event(socket, 'connect');
  const confirmed = event(socket, 'binary-confirmed');
  socket.once('binary-question', (value, reply) => {
    try {
      assert(Buffer.isBuffer(value));
      assert.deepStrictEqual(Array.from(value), [0, 255, 2]);
      reply(Buffer.from([2, 255, 0]));
    } catch (error) { finish(error); }
  });
  socket.connect();
  await connected;
  const echoed = await ack(socket, 'binary', Buffer.from([0, 1, 127, 128, 255]));
  assert(Buffer.isBuffer(echoed[0]));
  assert.deepStrictEqual(Array.from(echoed[0]), [0, 1, 127, 128, 255]);
  const values = await confirmed;
  assert(Buffer.isBuffer(values[0]));
  assert.deepStrictEqual(Array.from(values[0]), [2, 255, 0]);
  socket.close();
}
(async () => {
  for (const transport of ['polling', 'websocket']) await scenario(transport);
  console.log('example passed with socket.io-client ' + version);
})().then(() => finish(), error => finish(error));
