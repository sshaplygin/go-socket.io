package socketio

import (
	"sync"
)

func (nh *namespaceHandler) CopyRegistrations(child *namespaceHandler) {
	nh.eventsLock.RLock()
	defer nh.eventsLock.RUnlock()
	child.onConnect, child.onDisconnect, child.onError = nh.onConnect, nh.onDisconnect, nh.onError
	child.middleware = append([]func(Conn) error(nil), nh.middleware...)
	for event, listeners := range nh.events {
		copied := make([]*eventListener, 0, len(listeners))
		for _, listener := range listeners {
			copied = append(copied, &eventListener{handler: listener.handler, typed: listener.typed, once: listener.once})
		}
		child.events[event] = copied
	}
}

type namespaceRegistry struct {
	handlers map[string]*namespaceHandler
	mu       sync.RWMutex
}

func newNamespaceRegistry() *namespaceRegistry {
	return &namespaceRegistry{
		handlers: make(map[string]*namespaceHandler),
	}
}

func (h *namespaceRegistry) Set(namespace string, handler *namespaceHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.handlers[namespace] = handler
}

func (h *namespaceRegistry) Get(nsp string) (*namespaceHandler, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	handler, ok := h.handlers[nsp]
	return handler, ok
}

// Range calls f for every handler; f must not register handlers.
func (h *namespaceRegistry) Range(f func(handler *namespaceHandler)) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, handler := range h.handlers {
		f(handler)
	}
}
