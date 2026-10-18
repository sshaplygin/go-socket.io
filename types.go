package socketio

import (
	"github.com/sshaplygin/go-socket.io/client"
)

// AckFunc acknowledges an incoming event once, including after its handler returns.
type AckFunc = client.AckFunc

// Handshake describes the original engine handshake and namespace query.
type Handshake = client.Handshake

// Conn is the shared server and client connection type.
type Conn = client.Conn

// Namespace is the shared server and client interface.
type Namespace = client.Namespace

// Emitter is the shared server and client interface.
type Emitter = client.Emitter
