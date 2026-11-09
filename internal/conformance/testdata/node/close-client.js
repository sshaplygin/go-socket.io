'use strict';
// Connects to a Go server over websocket (or polling upgraded to websocket), asks
// the server to end the session and records the websocket close code the client
// reads. A server that stops the session in an orderly way sends a close frame
// (code 1000), so that a client can tell it from a dropped connection (1006).
const assert = require('assert');
const io = require('socket.io-client');
const version = require('socket.io-client/package.json').version;
assert(['1.7.4', '2.5.0'].includes(version), 'unexpected client version: ' + version);
if (process.env.CONFORMANCE_CLIENT_VERSION) {
  assert.strictEqual(version, process.env.CONFORMANCE_CLIENT_VERSION);
}

const [base, mode, trigger, compression] = process.argv.slice(2);
const payload = 'compression'.repeat(400);
const socket = io(base, {
  transports: mode === 'upgrade' ? ['polling', 'websocket'] : ['websocket'],
  perMessageDeflate: compression === 'deflate',
  forceNode: true, forceNew: true, reconnection: false, timeout: 5000
});

function once(emitter, name, timeout = 5000) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('event timed out: ' + name)), timeout);
    emitter.once(name, (...args) => { clearTimeout(timer); resolve(args); });
  });
}

const deadline = setTimeout(() => { console.error('close scenario timed out'); process.exit(1); }, 20000);

(async () => {
  try {
    const engine = socket.io.engine;
    const upgraded = mode === 'upgrade' ? once(engine, 'upgrade') : null;
    const connected = once(socket, 'connect');
    await connected;
    if (upgraded) { await upgraded; }
    assert.strictEqual(engine.transport.name, 'websocket');
    if (compression === 'deflate') {
      const reply = once(socket, 'reply');
      socket.emit('echo', payload);
      assert.deepStrictEqual(await reply, [payload]);
    }
    // The client's own handler maps every close to "transport close"; the
    // close event of the websocket carries the code.
    const ws = engine.transport.ws;
    const closed = new Promise(resolve => ws.addEventListener('close', resolve));
    const disconnected = once(socket, 'disconnect');
    socket.emit(trigger);
    const [reason] = await disconnected;
    const event = await closed;
    assert.strictEqual(event.code, 1000, 'websocket close code');
    assert.strictEqual(reason, 'transport close');
    console.log(version + ' ' + mode + ' ' + trigger + (compression ? ' ' + compression : '') +
      ': close code ' + event.code + ', reason "' + reason + '" passed');
  } finally {
    clearTimeout(deadline);
    socket.disconnect();
  }
})().catch(error => { console.error(error.stack || error); process.exit(1); });
