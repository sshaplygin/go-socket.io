package namespace

import (
	"sync"
)

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
