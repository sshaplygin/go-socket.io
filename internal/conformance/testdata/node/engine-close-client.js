'use strict';
// Connects to a Go server over websocket (or polling upgraded to websocket) and
// closes the engine directly, without a Socket.IO DISCONNECT: the websocket
// sends a close frame, and the Go test checks the reason the server reports.
const assert = require('assert');
const io = require('socket.io-client');
const version = require('socket.io-client/package.json').version;
assert(['1.7.4', '2.5.0'].includes(version), 'unexpected client version: ' + version);
if (process.env.CONFORMANCE_CLIENT_VERSION) {
  assert.strictEqual(version, process.env.CONFORMANCE_CLIENT_VERSION);
}

const [base, mode] = process.argv.slice(2);
const socket = io(base, {
  transports: mode === 'upgrade' ? ['polling', 'websocket'] : ['websocket'],
  forceNode: true, forceNew: true, reconnection: false, timeout: 5000
});

function once(emitter, name, timeout = 5000) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('event timed out: ' + name)), timeout);
    emitter.once(name, (...args) => { clearTimeout(timer); resolve(args); });
  });
}

const deadline = setTimeout(() => { console.error('engine close scenario timed out'); process.exit(1); }, 20000);

(async () => {
  try {
    const engine = socket.io.engine;
    const upgraded = mode === 'upgrade' ? once(engine, 'upgrade') : null;
    await once(socket, 'connect');
    if (upgraded) { await upgraded; }
    assert.strictEqual(engine.transport.name, 'websocket');
    // Wait for the server's reply to the close frame, so that the server has
    // read the frame before this process exits.
    const ws = engine.transport.ws;
    const closed = new Promise(resolve => ws.addEventListener('close', resolve));
    engine.close();
    const event = await closed;
    console.log(version + ' ' + mode + ': engine closed, close code ' + event.code);
  } finally {
    clearTimeout(deadline);
  }
})().catch(error => { console.error(error.stack || error); process.exit(1); });
