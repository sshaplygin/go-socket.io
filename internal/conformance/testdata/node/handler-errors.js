'use strict';
// Error results of untyped Go handlers reach the client's ACK callback as
// {code, message} objects; recovered panics reach it only as fixed text, and the
// panic values never do.
const assert = require('assert');
const io = require('socket.io-client');
const version = require('socket.io-client/package.json').version;
assert(['1.7.4', '2.5.0'].includes(version), 'unexpected client version: ' + version);
if (process.env.CONFORMANCE_CLIENT_VERSION) {
  assert.strictEqual(version, process.env.CONFORMANCE_CLIENT_VERSION);
}

const options = { transports: [process.argv[3]], reconnection: false, forceNew: true, forceNode: true, autoConnect: false };
const socket = io(process.argv[2] + '/chat', options);
const watchdog = setTimeout(() => { console.error('handler errors scenario timed out'); process.exit(1); }, 15000);
const seen = [];

function once(target, name) { return new Promise(resolve => target.once(name, resolve)); }
function ack(name, ...args) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('ACK callback was not called: ' + name)), 5000);
    socket.emit(name, ...args, (...values) => { clearTimeout(timer); seen.push(values); resolve(values); });
  });
}

(async () => {
  const connected = once(socket, 'connect'); socket.connect(); await connected;

  assert.deepStrictEqual(await ack('fail'), [{ code: 'handler_error', message: 'bad thing' }]);
  assert.deepStrictEqual(await ack('ok'), [null]);
  assert.deepStrictEqual(await ack('pair', true), ['', { code: 'handler_error', message: 'bad thing' }]);
  assert.deepStrictEqual(await ack('pair', false), ['done', null]);
  assert.deepStrictEqual(await ack('typed', 1), [{ code: 'handler_panic', message: 'internal error' }]);

  // Event middleware that panics answers with an ERROR packet; the socket stays connected.
  const rejected = once(socket, 'error');
  socket.emit('guarded');
  const text = await rejected;
  seen.push(text);
  assert.strictEqual(text, 'internal error');
  assert.deepStrictEqual(await ack('ok'), [null]);

  // A namespace whose OnConnect panics rejects the connection with the same text.
  const other = io(process.argv[2] + '/boom', options);
  const refused = once(other, 'error');
  other.connect();
  const refusal = await refused;
  seen.push(refusal);
  assert.strictEqual(refusal, 'internal error');
  other.close();

  // An untyped handler panic closes the connection.
  const gone = once(socket, 'disconnect');
  socket.emit('boom');
  await gone;

  assert(!JSON.stringify(seen).includes('secret'), 'a panic value reached the client: ' + JSON.stringify(seen));
  console.log(version + ' ' + process.argv[3] + ': handler errors passed');
})().catch(err => { console.error(err); process.exitCode = 1; }).finally(() => { clearTimeout(watchdog); socket.close(); });
