package transport

import (
	"net/http"
	"net/url"
)

// Transport accepts and dials transport connections.
type Transport interface {
	// Name returns the transport name used in the transport query parameter
	// and in upgrade lists, such as "polling" or "websocket".
	Name() string
	// Accept creates the server connection for the request r. A transport
	// that fails may already have answered on w, as a websocket handshake
	// error does.
	Accept(w http.ResponseWriter, r *http.Request) (Conn, error)
	// Dial opens a client connection to u, sending requestHeader with its
	// requests. It may modify u.
	Dial(u *url.URL, requestHeader http.Header) (Conn, error)
}

// Manager is a registry of transports by name, in upgrade order. Its methods do
// not change it, and it is safe for concurrent use while no caller writes to a
// slice that UpgradeFrom returned.
type Manager struct {
	order      []string
	transports map[string]Transport
}

// Option configures a transport manager.
type Option func(*managerOptions)

type managerOptions struct{ transports []Transport }

// WithTransports selects transports in upgrade order. The slice is copied.
func WithTransports(values ...Transport) Option {
	return func(config *managerOptions) { config.transports = append(values[:0:0], values...) }
}

// NewManager creates a manager. The default registry is empty.
func NewManager(opts ...Option) *Manager {
	var config managerOptions
	for _, option := range opts {
		if option != nil {
			option(&config)
		}
	}
	transports := config.transports
	tranMap := make(map[string]Transport)
	names := make([]string, len(transports))
	for i, t := range transports {
		names[i] = t.Name()
		tranMap[t.Name()] = t
	}

	return &Manager{
		order:      names,
		transports: tranMap,
	}
}

// UpgradeFrom returns a name list of transports which can upgrade from given
// name, or nil for a name that is not registered. The list is not a copy: it
// shares the manager's storage, so writing to it changes later results.
func (m *Manager) UpgradeFrom(name string) []string {
	for i, n := range m.order {
		if n == name {
			return m.order[i+1:]
		}
	}
	return nil
}

// Get returns the transport with given name.
func (m *Manager) Get(name string) (Transport, bool) {
	t, ok := m.transports[name]
	return t, ok
}
