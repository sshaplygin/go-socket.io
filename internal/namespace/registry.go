package namespace

import (
	"sync"
)

func (nh *Handler) CopyRegistrations(child *Handler) {
	nh.eventsLock.RLock()
	defer nh.eventsLock.RUnlock()
	child.onConnect, child.onDisconnect, child.onError = nh.onConnect, nh.onDisconnect, nh.onError
	child.middleware = append([]func(Conn) error(nil), nh.middleware...)
	for event, listeners := range nh.events {
		copied := make([]*Listener, 0, len(listeners))
		for _, listener := range listeners {
			copied = append(copied, &Listener{handler: listener.handler, once: listener.once})
		}
		child.events[event] = copied
	}
}

type Registry struct {
	handlers map[string]*Handler
	mu       sync.RWMutex
}

func NewRegistry() *Registry {
	return &Registry{
		handlers: make(map[string]*Handler),
	}
}

func (h *Registry) Set(namespace string, handler *Handler) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.handlers[namespace] = handler
}

func (h *Registry) Get(nsp string) (*Handler, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	handler, ok := h.handlers[nsp]
	return handler, ok
}

// Range calls f for every handler; f must not register handlers.
func (h *Registry) Range(f func(handler *Handler)) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, handler := range h.handlers {
		f(handler)
	}
}
