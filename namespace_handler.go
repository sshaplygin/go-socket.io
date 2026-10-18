package socketio

import (
	"errors"
	"reflect"
	"sync"

	"github.com/sshaplygin/go-socket.io/adapter"
	"github.com/sshaplygin/go-socket.io/parser"
)

type namespaceHandler struct {
	connectedMu sync.RWMutex
	connected   map[string]Conn
	view        *ServerNamespace
	middleware  []func(Conn) error
	broadcast   adapter.Adapter
	err         error // why broadcast is a nopBroadcast: the adapter could not be created

	events     map[string][]*eventListener
	eventsLock sync.RWMutex

	onConnect    func(conn Conn) error
	onDisconnect func(conn Conn, msg string)
	onError      func(conn Conn, err error)
}

func newNamespaceHandler() *namespaceHandler {
	return &namespaceHandler{broadcast: adapter.NewMemory(), connected: make(map[string]Conn), events: make(map[string][]*eventListener)}
}

// nopBroadcast is the adapter.Adapter of a namespace whose adapter could not be
// created: it keeps no rooms and sends nothing.
type nopBroadcast struct{}

func (nopBroadcast) Join(string, adapter.Socket) {}

func (nopBroadcast) Leave(string, adapter.Socket) {}

func (nopBroadcast) LeaveAll(adapter.Socket) {}

func (nopBroadcast) Clear(string) {}

func (nopBroadcast) Send(string, string, ...interface{}) {}

func (nopBroadcast) SendAll(string, ...interface{}) {}

func (nopBroadcast) ForEach(string, adapter.EachFunc) {}

func (nopBroadcast) Len(string) int { return -1 }

func (nopBroadcast) Rooms(adapter.Socket) []string { return nil }

func (nopBroadcast) AllRooms() []string { return nil }

func (nh *namespaceHandler) OnConnect(f func(Conn) error) {
	nh.eventsLock.Lock()
	defer nh.eventsLock.Unlock()
	nh.onConnect = f
}

func (nh *namespaceHandler) OnDisconnect(f func(Conn, string)) {
	nh.eventsLock.Lock()
	defer nh.eventsLock.Unlock()
	nh.onDisconnect = f
}

func (nh *namespaceHandler) OnError(f func(Conn, error)) {
	nh.eventsLock.Lock()
	defer nh.eventsLock.Unlock()
	nh.onError = f
}

type eventListener struct {
	handler *funcHandler
	once    bool
	fired   bool // protected by eventsLock; snapshots share this claim
}

func (nh *namespaceHandler) OnEvent(event string, f interface{}) {
	listener := &eventListener{handler: newEventFunc(f)}
	nh.eventsLock.Lock()
	defer nh.eventsLock.Unlock()
	nh.events[event] = []*eventListener{listener}
}

func (nh *namespaceHandler) addListener(event string, f interface{}, once bool) func() {
	listener := &eventListener{handler: newEventFunc(f), once: once}
	nh.eventsLock.Lock()
	defer nh.eventsLock.Unlock()
	current := nh.events[event]
	if len(current) > 0 && !reflect.DeepEqual(current[0].handler.argTypes, listener.handler.argTypes) {
		panic("event listeners must have identical argument types")
	}
	nh.events[event] = append(current, listener)
	return func() {
		nh.eventsLock.Lock()
		defer nh.eventsLock.Unlock()
		nh.removeListener(event, listener)
	}
}

func (nh *namespaceHandler) removeListener(event string, target *eventListener) {
	current := nh.events[event]
	for i, listener := range current {
		if listener == target {
			next := append([]*eventListener(nil), current[:i]...)
			next = append(next, current[i+1:]...)
			if len(next) == 0 {
				delete(nh.events, event)
			} else {
				nh.events[event] = next
			}
			return
		}
	}
}

func (nh *namespaceHandler) Off(event string) {
	nh.eventsLock.Lock()
	defer nh.eventsLock.Unlock()
	delete(nh.events, event)
}

func (nh *namespaceHandler) listeners(event string) []*eventListener {
	nh.eventsLock.RLock()
	defer nh.eventsLock.RUnlock()
	return append([]*eventListener(nil), nh.events[event]...)
}

func (nh *namespaceHandler) getEventTypes(event string) []reflect.Type {
	listeners := nh.listeners(event)
	if len(listeners) != 0 {
		return listeners[0].handler.argTypes
	}
	return nil
}

func (nh *namespaceHandler) dispatch(conn Conn, header parser.Header, args ...reflect.Value) ([]reflect.Value, error) {
	switch header.Type {
	case parser.Connect:
		if nh.onConnect != nil {
			return nil, nh.onConnect(conn)
		}
		return nil, nil

	case parser.Disconnect:
		if nh.onDisconnect != nil {
			nh.onDisconnect(conn, getDispatchMessage(args...))
		}
		return nil, nil

	case parser.Error:
		if nh.onError != nil {
			msg := getDispatchMessage(args...)
			if msg == "" {
				msg = "parser error dispatch"
			}
			nh.onError(conn, errors.New(msg))
		}
	}

	return nil, parser.ErrInvalidPacketType
}

func (nh *namespaceHandler) dispatchEvent(conn Conn, event string, args ...reflect.Value) ([]reflect.Value, error) {
	ret, _, err := nh.dispatchListeners(conn, event, nh.listeners(event), func(...interface{}) {}, args...)
	return ret, err
}

func (nh *namespaceHandler) dispatchListeners(conn Conn, event string, snapshot []*eventListener, ack AckFunc, args ...reflect.Value) ([]reflect.Value, bool, error) {
	var result []reflect.Value
	ownsAck := false
	for _, listener := range snapshot {
		if listener.once {
			// Claim immediately before this callback. Earlier listeners may fail, in
			// which case later Once listeners remain available for the next event.
			nh.eventsLock.Lock()
			if listener.fired {
				nh.eventsLock.Unlock()
				continue
			}
			listener.fired = true
			nh.removeListener(event, listener)
			nh.eventsLock.Unlock()
		}
		ownsAck = ownsAck || listener.handler.ack
		values := make([]reflect.Value, 1, len(args)+2)
		values[0] = reflect.ValueOf(conn)
		values = append(values, args...)
		if listener.handler.ack {
			values = append(values, reflect.ValueOf(ack))
		}
		ret, err := listener.handler.Call(values)
		if err != nil {
			return nil, ownsAck, err
		}
		if len(result) == 0 && len(ret) != 0 {
			result = ret
		}
	}
	return result, ownsAck, nil
}

func getDispatchMessage(args ...reflect.Value) string {
	var msg string
	if len(args) > 0 {
		msg = args[0].Interface().(string)
	}

	return msg
}

func (nh *namespaceHandler) connect(c Conn) error {
	nh.eventsLock.RLock()
	middleware := append([]func(Conn) error(nil), nh.middleware...)
	onConnect := nh.onConnect
	nh.eventsLock.RUnlock()
	for _, f := range middleware {
		if err := f(c); err != nil {
			return err
		}
	}
	if onConnect != nil {
		return onConnect(c)
	}
	return nil
}

func (nh *namespaceHandler) addSocket(c Conn) {
	nh.connectedMu.Lock()
	nh.connected[c.ID()] = c
	nh.connectedMu.Unlock()
}

func (nh *namespaceHandler) removeSocket(c Conn) {
	nh.connectedMu.Lock()
	if nh.connected[c.ID()] == c {
		delete(nh.connected, c.ID())
	}
	nh.connectedMu.Unlock()
}

func (nopBroadcast) SendTo(adapter.Options, string, ...interface{}) error { return nil }

func (nopBroadcast) IDs(...string) []string { return nil }

func (nh *namespaceHandler) copyRegistrations(child *namespaceHandler) {
	nh.eventsLock.RLock()
	defer nh.eventsLock.RUnlock()
	child.onConnect, child.onDisconnect, child.onError = nh.onConnect, nh.onDisconnect, nh.onError
	child.middleware = append([]func(Conn) error(nil), nh.middleware...)
	for event, listeners := range nh.events {
		copied := make([]*eventListener, 0, len(listeners))
		for _, listener := range listeners {
			copied = append(copied, &eventListener{handler: listener.handler, once: listener.once})
		}
		child.events[event] = copied
	}
}

type namespaceHandlers struct {
	handlers map[string]*namespaceHandler
	mu       sync.RWMutex
}

func newNamespaceHandlers() *namespaceHandlers {
	return &namespaceHandlers{
		handlers: make(map[string]*namespaceHandler),
	}
}

func (h *namespaceHandlers) Set(namespace string, handler *namespaceHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.handlers[namespace] = handler
}

func (h *namespaceHandlers) Get(nsp string) (*namespaceHandler, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	handler, ok := h.handlers[nsp]
	return handler, ok
}

// Range calls f for every handler; f must not register handlers.
func (h *namespaceHandlers) Range(f func(handler *namespaceHandler)) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, handler := range h.handlers {
		f(handler)
	}
}
