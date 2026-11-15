'use strict';
const http = require('http');
const assert = require('assert');
const socketio = require('socket.io');
assert.strictEqual(require('socket.io/package.json').version, '2.5.0');
const server = http.createServer();
const io = socketio(server);
for (const name of ['/', '/chat', '/welcome']) {
  const namespace = io.of(name);
  namespace.on('connection', socket => {
    if (name === '/welcome' || socket.handshake.query.welcome === '1') {
      // Broadcasts from the connection listener that select the connecting socket reach it.
      socket.join('lobby');
      namespace.emit('welcome', 'all');
      namespace.to(socket.id).emit('welcome', 'self');
      namespace.to('lobby').emit('welcome', 'lobby');
      socket.broadcast.emit('welcome', 'others');
      socket.emit('welcome', 'direct');
    }
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
    socket.on('binary-many', (values, ack) => { socket.emit('binary-many-event', values); ack(values); });
    socket.on('ask-binary-many', ack => {
      const values = Array.from({length: 10}, (_, i) => Buffer.from([i, 255 - i]));
      socket.emit('binary-many-question', values, value => socket.emit('binary-many-result', value));
      ack();
    });
    socket.on('burst', (count, size, ack) => {
      const filler = 'x'.repeat(size);
      for (let i = 0; i < count; i++) socket.emit('item', i, filler);
      ack('sent');
    });
    let counted = 0;
    socket.on('count', () => { counted++; });
    socket.on('counted', ack => ack(counted));
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
