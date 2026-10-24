// Package redis provides a namespace adapter compatible with socket.io-redis 5.4.0.
package redis

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/sshaplygin/go-socket.io/adapter"
)

// options is private Redis configuration.
type options struct {
	RequestsTimeout      time.Duration
	CustomRequestHandler func(context.Context, json.RawMessage) (json.RawMessage, error)

	Addr     string
	Prefix   string
	Network  string
	Password string
	// DB : specifies the database to select when dialing a connection.
	DB int
}

func defaultOptions() *options {
	return &options{
		Addr:    "127.0.0.1:6379",
		Prefix:  "socket.io",
		Network: "tcp",
	}
}

func getOptions(opts *options) *options {
	options := defaultOptions()

	if opts != nil {
		options.DB = opts.DB
		options.RequestsTimeout = opts.RequestsTimeout
		options.CustomRequestHandler = opts.CustomRequestHandler
		if opts.Addr != "" {
			options.Addr = opts.Addr
		}

		if opts.Prefix != "" {
			options.Prefix = opts.Prefix
		}

		if opts.Network != "" {
			options.Network = opts.Network
		}

		if len(opts.Password) > 0 {
			options.Password = opts.Password
		}
	}

	return options
}

// Option configures a Redis namespace factory. Options apply in order.
type Option func(*options)

// New returns a lazy namespace factory interoperable with socket.io-redis 5.4.0.
// Namespaces share one subscriber and publishing pool. Construction waits for
// subscription acknowledgements within ten seconds; lost subscriptions reconnect
// with bounded backoff. WithDB selects the Redis database, but Pub/Sub isolation
// requires distinct prefixes.
//
// Values implement Cluster for bounded context/error query and control methods.
// IDs and AllRooms return partial snapshots and log errors; Len returns -1
// on failure. Clear performs bounded, acknowledged remote leaves. Queries are not
// atomic snapshots, and Clear does not roll back partial effects.
func New(opts ...Option) adapter.Factory {
	config := new(options)
	for _, option := range opts {
		if option != nil {
			option(config)
		}
	}
	resolved := getOptions(config)
	var creation sync.Mutex
	var resources *sharedResources
	return func(namespace string) (adapter.Adapter, error) {
		creation.Lock()
		defer creation.Unlock()
		if resources != nil {
			select {
			case <-resources.owner.done:
				resources = nil
			default:
			}
		}
		if resources != nil {
			return resources.add(namespace, resolved)
		}
		shared := &sharedResources{members: make(map[string][]*broadcast)}
		instance, err := newBroadcastWithResources(namespace, resolved, shared)
		if err != nil {
			return nil, err
		}
		resources = shared
		return instance, nil
	}
}

// WithAddr sets the Redis address; empty selects 127.0.0.1:6379.
func WithAddr(value string) Option { return func(config *options) { config.Addr = value } }

// WithPrefix sets channel names; empty selects socket.io.
func WithPrefix(value string) Option { return func(config *options) { config.Prefix = value } }

// WithNetwork sets the dial network; empty selects tcp.
func WithNetwork(value string) Option { return func(config *options) { config.Network = value } }

// WithPassword sets authentication; empty disables AUTH.
func WithPassword(value string) Option { return func(config *options) { config.Password = value } }

// WithDB selects the database on subscriber, pooled and request connections.
// Redis Pub/Sub channels are shared across databases; use WithPrefix for isolation.
func WithDB(value int) Option { return func(config *options) { config.DB = value } }

// WithRequestsTimeout bounds each cluster request and each broadcast PUBLISH,
// including Redis commands. Publication errors are returned by SendTo. The
// bound applies per I/O phase (dial, write, read), so a stalled publication can
// take up to about two to three times the timeout.
// Nonpositive values select the default five seconds.
func WithRequestsTimeout(value time.Duration) Option {
	return func(config *options) { config.RequestsTimeout = value }
}

// WithCustomRequestHandler answers one JSON-only custom request per shared
// namespace subscriber. A nil handler returns JSON null. Handlers must honor ctx.
func WithCustomRequestHandler(handler func(context.Context, json.RawMessage) (json.RawMessage, error)) Option {
	return func(config *options) { config.CustomRequestHandler = handler }
}
