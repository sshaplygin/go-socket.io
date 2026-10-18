package connection

import (
	"github.com/sshaplygin/go-socket.io/engineio"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
	"github.com/sshaplygin/go-socket.io/logger"
)

const defaultWriteBufferSize = 64

// testOption configures only owner-local fixture inputs. Public constructors are
// exercised independently in package client and package client_test.
type testConfig struct {
	Config
	Logger *slog.Logger
}
type testOption func(*testConfig)

func resolveOptions(opts ...testOption) *testConfig {
	c := new(testConfig)
	for _, o := range opts {
		if o != nil {
			o(c)
		}
	}
	return c
}
func newTestManager(addr string, opts ...testOption) (*Manager, error) {
	u, err := url.Parse(addr)
	if err != nil {
		return nil, err
	}
	resolved := resolveOptions(opts...)
	config := resolved.Config
	if config.Timeout == 0 {
		config.Timeout = 20 * time.Second
	}
	if config.ReconnectionDelay <= 0 {
		config.ReconnectionDelay = time.Second
	}
	if config.ReconnectionDelayMax <= 0 {
		config.ReconnectionDelayMax = 5 * time.Second
	}
	if config.RandomizationFactor == 0 {
		config.RandomizationFactor = .5
	}
	if config.WriteBufferSize <= 0 {
		config.WriteBufferSize = defaultWriteBufferSize
	}
	if config.CloseTimeout <= 0 {
		config.CloseTimeout = time.Minute
	}
	if config.Transports == nil {
		config.Transports = []transport.Transport{polling.Default, websocket.Default}
	}
	nsp := fmtNS(u.Path)
	u.Path, u.RawPath = strings.TrimRight("/socket.io", "/")+"/", ""
	log := logger.Log
	if resolved.Logger != nil {
		log = logger.Wrap(resolved.Logger)
	}
	return NewManager(u.String(), nsp, config, log, nil), nil
}

func WithTimeout(v time.Duration) testOption { return func(c *testConfig) { c.Timeout = v } }

func WithWriteBufferSize(v int) testOption { return func(c *testConfig) { c.WriteBufferSize = v } }

func WithCloseTimeout(v time.Duration) testOption { return func(c *testConfig) { c.CloseTimeout = v } }

func WithLogger(v *slog.Logger) testOption { return func(c *testConfig) { c.Logger = v } }

func WithReconnection(v bool) testOption { return func(c *testConfig) { c.DisableReconnection = !v } }

func WithReconnectionAttempts(v int) testOption {
	return func(c *testConfig) { c.ReconnectionAttempts = v }
}

func WithReconnectionDelay(v time.Duration) testOption {
	return func(c *testConfig) { c.ReconnectionDelay = v }
}

func WithReconnectionDelayMax(v time.Duration) testOption {
	return func(c *testConfig) { c.ReconnectionDelayMax = v }
}

func WithRandomizationFactor(v float64) testOption {
	return func(c *testConfig) { c.RandomizationFactor = v }
}

func WithTransports(v ...transport.Transport) testOption {
	return func(c *testConfig) { c.Transports = append([]transport.Transport(nil), v...) }
}

func shortPingOptions() []engineio.Option {
	return []engineio.Option{engineio.WithPingTimeout(500 * time.Millisecond), engineio.WithPingInterval(200 * time.Millisecond), engineio.WithLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))}
}
