package socketio

import "github.com/sshaplygin/go-socket.io/internal/namespace"

// AckFunc acknowledges an incoming event once, possibly from another goroutine
// after the handler returns. It does nothing unless the peer requested an ACK.
// A handler with this final parameter owns the ACK; return values are ignored.
type AckFunc = namespace.AckFunc

// Handshake describes the original engine handshake and this namespace's query.
type Handshake = namespace.Handshake

// Conn is a server-side Socket.IO namespace socket.
type Conn = namespace.Conn

// Namespace describes a communication channel that allows you to split the logic of your application
// over a single shared connection.
type Namespace = namespace.Namespace

// Emitter is an immutable pending target and its flags. Each builder returns a new
// value, so an Emitter can be reused. To or Except converts a single-socket target
// to a broadcast excluding its sender; Volatile, Local and Compress preserve its target.
type Emitter = namespace.Emitter
