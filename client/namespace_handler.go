package client

import (
	"errors"
	"reflect"
	"sync"

	"github.com/sshaplygin/go-socket.io/parser"
)

type namespaceHandler struct {
	broadcast    *roomIndex
	events       map[string][]*eventListener
	eventsLock   sync.RWMutex
	onConnect    func(Conn) error
	onDisconnect func(Conn, string)
	onError      func(Conn, error)
}

func newNamespaceHandler(string) *namespaceHandler {
	return &namespaceHandler{broadcast: newRoomIndex(), events: make(map[string][]*eventListener)}
}

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

func (nh *namespaceHandler) connectHandler() func(Conn) error {
	nh.eventsLock.RLock()
	defer nh.eventsLock.RUnlock()
	return nh.onConnect
}

func (nh *namespaceHandler) disconnectHandler() func(Conn, string) {
	nh.eventsLock.RLock()
	defer nh.eventsLock.RUnlock()
	return nh.onDisconnect
}

func (nh *namespaceHandler) errorHandler() func(Conn, error) {
	nh.eventsLock.RLock()
	defer nh.eventsLock.RUnlock()
	return nh.onError
}

type eventListener struct {
	handler *funcHandler
	once    bool
	fired   bool // protected by eventsLock
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

func (nh *namespaceHandler) dispatch(conn Conn, header parser.Header, args ...reflect.Value) ([]reflect.Value, error) {
	switch header.Type {
	case parser.Connect:
		if f := nh.connectHandler(); f != nil {
			return nil, f(conn)
		}
		return nil, nil

	case parser.Disconnect:
		if f := nh.disconnectHandler(); f != nil {
			f(conn, getDispatchMessage(args...))
		}
		return nil, nil

	case parser.Error:
		if f := nh.errorHandler(); f != nil {
			msg := getDispatchMessage(args...)
			if msg == "" {
				msg = "parser error dispatch"
			}
			f(conn, errors.New(msg))
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
