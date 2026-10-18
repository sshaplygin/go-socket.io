package client

import (
	"github.com/sshaplygin/go-socket.io/client/internal/connection"
	"github.com/sshaplygin/go-socket.io/client/internal/namespace"
)

// AckFunc acknowledges an incoming event once.
type AckFunc = namespace.AckFunc

// Handshake describes namespace metadata.
type Handshake = namespace.Handshake

// Conn is a client-side Socket.IO namespace socket.
type Conn = namespace.Conn

// Namespace is a client communication channel.
type Namespace = namespace.Namespace

// Emitter is an immutable pending target and its flags.
type Emitter = namespace.Emitter

// Socket is one manager-owned namespace on a Client.
type Socket = connection.Socket
