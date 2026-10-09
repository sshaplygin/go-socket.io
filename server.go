// Package socketio is the v2 API skeleton of a Socket.IO server for Go: the typed
// event descriptors, the Server, Namespace and Socket model, the Adapter contract,
// the observer hooks and the options, as declarations only.
//
// The skeleton has no runtime. Every operation that would register a handler,
// authenticate, connect, send, acknowledge, join a room or close returns
// ErrNotImplemented, and NewServer returns no server. The runtime is added by the
// stages listed in docs/ROADMAP.md (2.1 to 2.4). The v1 server, with the reflection
// based API, lives on the branch v1.x.
package socketio

import (
	"context"
	"net/http"
)

// Server owns the namespaces and the Engine.IO sessions. The skeleton holds neither.
type Server struct{}

// NewServer validates opts and returns a server. It creates no namespace, "/"
// included: a CONNECT to a namespace the application has not created with
// Server.Namespace is unknown. The skeleton returns a nil server and
// ErrNotImplemented.
func NewServer(opts Options) (*Server, error) { return nil, ErrNotImplemented }

// Namespace is the creating call. It returns the namespace called name, creating it
// with Options.Adapter on first use. Once shutdown has begun it returns an error
// matching ErrNamespaceClosed; a registered namespace is returned without calling the
// factory; a creation in progress is waited for; ctx bounds only the caller's wait,
// not the creation. The ordering rules are in docs/ROADMAP.md, section 2.2
// (Readiness). The skeleton returns a nil namespace and ErrNotImplemented.
func (*Server) Namespace(ctx context.Context, name string) (*Namespace, error) {
	return nil, ErrNotImplemented
}

// Shutdown rejects new work, drains queued outbound messages until ctx ends and then
// closes transports and owned adapters. It is idempotent. The skeleton always returns
// ErrNotImplemented.
func (*Server) Shutdown(context.Context) error { return ErrNotImplemented }

// Close aborts immediately without draining. The skeleton always returns
// ErrNotImplemented.
func (*Server) Close() error { return ErrNotImplemented }

var _ http.Handler = (*Server)(nil)

// ServeHTTP implements http.Handler: the Engine.IO endpoint of the server (roadmap
// 2.1, with the Socket.IO layer from 2.3S). The skeleton answers 501 Not Implemented
// and reads nothing from the request.
func (*Server) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, ErrNotImplemented.Error(), http.StatusNotImplemented)
}
