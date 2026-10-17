'use strict';
const assert = require('assert');
const http = require('http');
const io = require('socket.io-client');
const payload = 'compression'.repeat(400);
let negotiated = '';
const originals = {request: http.request, get: http.get};
// The pinned ws versions use request/get respectively. Record the actual
// response; the older ws does not emit its own upgrade event.
for (const method of ['request', 'get']) {
  http[method] = function () {
    const req = originals[method].apply(this, arguments);
    req.once('upgrade', response => { negotiated = response.headers['sec-websocket-extensions'] || ''; });
    return req;
  };
}
const socket = io(process.argv[2], {transports: ['websocket'], perMessageDeflate: true, forceNode: true, forceNew: true, reconnection: false});
let event = false;
const timer = setTimeout(() => finish(new Error('compression scenario timed out')), 10000);
function finish(error) {
  clearTimeout(timer);
  Object.assign(http, originals);
  socket.close();
  if (error) { console.error(error.stack || error); process.exit(1); }
  console.log('PASS permessage-deflate 4KB round trip, recorded upgrade: ' + negotiated);
}
socket.on('connect_error', finish);
socket.on('reply', value => {
  try { assert.strictEqual(value, payload); event = true; } catch (error) { finish(error); }
});
socket.on('connect', () => {
  try { assert(negotiated.includes('permessage-deflate'), 'extension missing from upgrade response'); }
  catch (error) { finish(error); return; }
  socket.emit('echo', payload, value => {
    try { assert.strictEqual(value, payload); assert(event, 'server event missing before ACK'); finish(); }
    catch (error) { finish(error); }
  });
});
