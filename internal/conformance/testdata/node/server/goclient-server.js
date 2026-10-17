'use strict';
const assert = require('assert');
const http = require('http');
const socketio = require('socket.io');
assert.strictEqual(require('socket.io/package.json').version, '2.5.0');
const server = http.createServer();
const io = socketio(server, {path: '/client-engine'});
const sessions = new Map();
for (const name of ['/', '/chat', '/late']) {
  io.of(name).on('connection', socket => {
    const sid = socket.conn.id;
    let state = sessions.get(sid);
    if (!state) {
      state = {connected: [], disconnected: []};
      sessions.set(sid, state);
      socket.conn.once('close', () => sessions.delete(sid));
    }
    state.connected.push(socket.nsp.name);
    socket.on('disconnect', reason => state.disconnected.push([socket.nsp.name, reason]));
    socket.on('echo', (value, ack) => { socket.emit('reply', value); ack(value); });
    socket.on('binary', (value, ack) => ack(value));
    socket.on('ask', () => socket.emit('question', 'server', value => socket.emit('answer', value)));
    socket.on('inspect', ack => ack({
      sid, id: socket.id, namespace: socket.nsp.name,
      transport: socket.conn.transport.name,
      query: socket.handshake.query.custom,
      namespaceQuery: socket.handshake.query.namespace,
      header: socket.handshake.headers['x-go-client'],
      url: socket.handshake.url,
      connected: state.connected, disconnected: state.disconnected
    }));
    socket.on('await-upgrade', ack => {
      if (socket.conn.transport.name === 'websocket') ack('websocket');
      else socket.conn.once('upgrade', transport => ack(transport.name));
    });
  });
}
server.listen(0, '127.0.0.1', () => console.log('http://127.0.0.1:' + server.address().port));
