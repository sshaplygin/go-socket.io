'use strict';
const http = require('http');
const assert = require('assert');
const socketio = require('socket.io');
assert.strictEqual(require('socket.io/package.json').version, '2.5.0');
const server = http.createServer();
const io = socketio(server);
for (const name of ['/', '/chat']) {
  const namespace = io.of(name);
  namespace.on('connection', socket => {
    socket.on('echo', (value, ack) => ack(value));
    socket.on('identity', ack => ack(socket.id));
    socket.on('metadata', ack => ack({query: socket.handshake.query.custom,
      header: socket.handshake.headers['x-differential'],
      time: socket.handshake.time, issued: socket.handshake.issued}));
    socket.on('ask', ack => {
      socket.emit('question', 'server', value => socket.emit('ack-result', value));
      ack();
    });
    socket.on('binary', (value, ack) => { socket.emit('binary-event', value); ack(value); });
    socket.on('ask-binary', ack => {
      socket.emit('binary-question', Buffer.from([0, 255, 2]), value => socket.emit('binary-result', value));
      ack();
    });
    socket.on('join', (room, ack) => socket.join(room, () => ack('joined')));
    socket.on('leave', (room, ack) => socket.leave(room, () => ack('left')));
    socket.on('room', (room, except, ack) => {
      (except ? socket.to(room) : namespace.to(room)).emit('news', except ? 'others' : 'all');
      namespace.emit('barrier');
      ack('sent');
    });
    socket.on('disconnect-one', () => socket.disconnect(false));
    socket.on('disconnect-all', () => socket.disconnect(true));
    socket.on('drop-transport', () => socket.conn.close());
  });
}
io.of('/denied').use((socket, next) => next(new Error('access denied')));
server.listen(0, '127.0.0.1', () => console.log('http://127.0.0.1:' + server.address().port));
