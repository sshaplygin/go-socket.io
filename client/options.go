package client

import (
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio/transport"
)

// Options configures a Client made by New.
type Options struct {
	// Transports are tried in upgrade order. Nil means polling then websocket.
	Transports []transport.Transport
	// Path is the Engine.IO endpoint; empty means /socket.io.
	Path string
	// Query is added to every engine request, separate from namespace queries.
	Query url.Values
	// Header is sent with every request, including a websocket upgrade.
	Header http.Header
	// Timeout bounds the dial through OPEN. Zero means 20 seconds; negative is unbounded.
	Timeout time.Duration
	// WriteBufferSize bounds queued writes and pre-connect events; nonpositive means 64.
	WriteBufferSize int
	Logger          *slog.Logger
}
