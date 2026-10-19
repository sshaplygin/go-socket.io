package client

import (
	"sync"
)

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
