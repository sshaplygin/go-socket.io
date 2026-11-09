'use strict';
// An event whose arguments do not decode into the Go handler's parameter types
// must still call the client's ACK callback, with the error-first ACK that typed
// acknowledged handlers send. socket.io-client 1.x and 2.x have no ACK timeout,
// so a missing reply would leave the callback pending forever.
const assert = require('assert');
const io = require('socket.io-client');
const version = require('socket.io-client/package.json').version;
assert(['1.7.4', '2.5.0'].includes(version), 'unexpected client version: ' + version);
if (process.env.CONFORMANCE_CLIENT_VERSION) {
  assert.strictEqual(version, process.env.CONFORMANCE_CLIENT_VERSION);
}

const socket = io(process.argv[2] + '/chat', {
  transports: [process.argv[3]], reconnection: false, forceNew: true, forceNode: true, autoConnect: false
});
const watchdog = setTimeout(() => { console.error('ack mismatch scenario timed out'); process.exit(1); }, 15000);

function once(name) { return new Promise(resolve => socket.once(name, resolve)); }
function ack(name, ...args) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('ACK callback was not called: ' + name)), 5000);
    socket.emit(name, ...args, (...values) => { clearTimeout(timer); resolve(values); });
  });
}
function requireInvalidPayload(values, argument) {
  assert.strictEqual(values.length, 1);
  assert.deepStrictEqual(Object.keys(values[0]).sort(), ['code', 'message']);
  assert.strictEqual(values[0].code, 'invalid_payload');
  assert(values[0].message.startsWith('decode argument ' + argument + ': json: cannot unmarshal '), values[0].message);
}

(async () => {
  const connected = once('connect'); socket.connect(); await connected;

  requireInvalidPayload(await ack('count', 'not a number'), 0);
  requireInvalidPayload(await ack('pair', 1, 2), 1);
  requireInvalidPayload(await ack('count', { nested: [1, 2] }), 0);
  requireInvalidPayload(await ack('count', Buffer.from([0, 255, 7])), 0);

  // Arguments that decode are acknowledged by the handler, including a missing
  // or an extra argument.
  assert.deepStrictEqual(await ack('count', 41), [42]);
  assert.deepStrictEqual(await ack('count'), [1]);
  assert.deepStrictEqual(await ack('count', 41, 'extra'), [42]);

  // Without an ACK callback nothing is answered, and the connection stays usable.
  socket.emit('count', 'not a number');
  socket.emit('count', Buffer.from([1]));
  assert.deepStrictEqual(await ack('count', 1), [2]);

  console.log(version + ' ' + process.argv[3] + ': ack mismatch passed');
})().catch(err => { console.error(err); process.exitCode = 1; }).finally(() => { clearTimeout(watchdog); socket.close(); });
