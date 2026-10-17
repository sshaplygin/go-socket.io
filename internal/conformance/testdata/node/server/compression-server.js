'use strict';
const assert = require('assert');
const http = require('http');
const socketio = require('socket.io');
assert.strictEqual(require('socket.io/package.json').version, '2.5.0');
const server = http.createServer();
const io = socketio(server, {perMessageDeflate: true});
io.on('connection', socket => {
  socket.on('echo', (value, ack) => {
    assert(value.length >= 4096);
    assert(socket.conn.transport.socket.extensions.includes('permessage-deflate'));
    socket.emit('reply', value);
    ack({value, extension: socket.conn.transport.socket.extensions});
  });
});
server.listen(0, '127.0.0.1', () => console.log('http://127.0.0.1:' + server.address().port));
