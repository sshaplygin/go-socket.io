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
  if (error) { console.error(error); process.exitCode = 1; }
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
  socket.on('connect_error', error => { finish(error); process.exit(1); });
  const connected = event(socket, 'connect');
  socket.connect();
  await connected;
  assert.deepStrictEqual(await ack(socket, 'sum', 4, 5), [9]);
  assert.deepStrictEqual(await ack(socket, 'deferred', 'hello €👍'), ['received: hello €👍']);
  socket.close();
}
(async () => {
  for (const transport of ['polling', 'websocket']) await scenario(transport);
  console.log('example passed with socket.io-client ' + version);
})().then(() => finish(), error => finish(error));
