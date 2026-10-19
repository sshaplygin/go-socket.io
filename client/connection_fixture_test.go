package client

import (
	"github.com/sshaplygin/go-socket.io/engineio"
	"io"
	"log/slog"
	"time"
)

func newTestManager(addr string, opts ...Option) (*manager, error) {
	client, err := New(addr, opts...)
	if err != nil {
		return nil, err
	}
	return client.manager, nil
}

func shortPingOptions() []engineio.Option {
	return []engineio.Option{engineio.WithPingTimeout(500 * time.Millisecond), engineio.WithPingInterval(200 * time.Millisecond), engineio.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))}
}
