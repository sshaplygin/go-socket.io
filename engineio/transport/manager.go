package transport

import (
	"net/http"
	"net/url"
)

// Transport is a transport which can creates base.Conn
type Transport interface {
	Name() string
	Accept(w http.ResponseWriter, r *http.Request) (Conn, error)
	Dial(u *url.URL, requestHeader http.Header) (Conn, error)
}

// Manager is a manager of transports.
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
// name.
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
