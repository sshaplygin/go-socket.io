// Package redis provides a namespace adapter using the legacy Go Redis wire format.
package redis

import (
	"fmt"

	"github.com/sshaplygin/go-socket.io/adapter"
)

// options is private Redis configuration.
type options struct {
	// deprecated. Usage Addr options
	Host string
	// deprecated. Usage Addr options
	Port     string
	Addr     string
	Prefix   string
	Network  string
	Password string
	// DB : specifies the database to select when dialing a connection.
	DB int
}

func (ro *options) getAddr() string {
	if ro.Addr == "" {
		ro.Addr = fmt.Sprintf("%s:%s", ro.Host, ro.Port)
	}
	return ro.Addr
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
		if opts.Host != "" {
			options.Host = opts.Host
		}

		if opts.Port != "" {
			options.Port = opts.Port
		}

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

// New returns a namespace factory. It performs no I/O until applied to a namespace.
// Each namespace dials publishing and subscriber connections within ten seconds.
// Subscription commands are sent before construction returns, but remote delivery
// starts only once Redis has registered them. Lost subscriptions reconnect with
// bounded backoff. Server.Serve reports the first namespace construction error.
// The legacy adapter currently always uses database zero, including with WithDB.
//
// Len and AllRooms ask the Redis subscribers for their local count or rooms. They
// wait until all counted subscribers answer, or five seconds, then return the
// partial sum or union. Len returns -1 on command errors; AllRooms returns an
// empty list on publish failure. A missing subscription may cause the full wait
// and omit this instance's own data. A subscriber that registers between the count
// and publish can answer without being counted, completing the request early.
// Neither query provides an atomic cluster snapshot.
func New(opts ...Option) adapter.Factory {
	config := new(options)
	for _, option := range opts {
		if option != nil {
			option(config)
		}
	}
	resolved := getOptions(config)
	return func(namespace string) (adapter.Adapter, error) {
		instance, err := newBroadcast(namespace, resolved)
		if err != nil {
			return nil, err
		}
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

// WithHost retains the legacy host setting. The default address takes precedence.
// Deprecated: use WithAddr.
func WithHost(value string) Option { return func(config *options) { config.Host = value } }

// WithPort retains the legacy port setting. The default address takes precedence.
// Deprecated: use WithAddr.
func WithPort(value string) Option { return func(config *options) { config.Port = value } }

// WithDB retains the database setting. The current adapter normalizes it to DB0.
func WithDB(value int) Option { return func(config *options) { config.DB = value } }
