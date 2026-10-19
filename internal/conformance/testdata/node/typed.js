'use strict';
const assert = require('assert');
const io = require('socket.io-client');
const socket = io(process.argv[2] + '/chat', {
  transports: [process.argv[3]], reconnection: false, forceNew: true, autoConnect: false
});
const watchdog = setTimeout(() => { console.error('typed scenario timed out'); socket.close(); process.exitCode = 1; }, 10000);
function once(name) { return new Promise(resolve => socket.once(name, resolve)); }
function ack(name, value) { return new Promise(resolve => socket.emit(name, value, (...args) => resolve(args))); }
function check(value) {
  assert.strictEqual(value.Number, 42);
  assert(Buffer.isBuffer(value.Binary));
  assert.deepStrictEqual([...value.Binary], [0,255]);
  assert(Buffer.isBuffer(value.Nested.bytes));
  assert.deepStrictEqual([...value.Nested.bytes], [1,254]);
}
socket.on('typed-question', (value, reply) => { check(value); reply(null,value); });
(async () => {
  const connected = once('connect'); socket.connect(); await connected;
  const value = {Number:42, Binary:Buffer.from([0,255]), Nested:{bytes:Buffer.from([1,254])}};
  const success = await ack('typed-echo',value);
  assert.strictEqual(success.length,2); assert.strictEqual(success[0],null); check(success[1]);
  const failure = await ack('typed-rejected','request');
  assert.strictEqual(failure.length,1); assert.deepStrictEqual(failure[0],{code:'refused',message:'no'});
  const finished = once('typed-finished'); socket.emit('typed-start'); await finished;
  console.log('typed error-first ACK and nested binary passed');
})().catch(err => { console.error(err); process.exitCode = 1; }).finally(() => { clearTimeout(watchdog); socket.close(); });
