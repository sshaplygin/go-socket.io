package client

import (
	"reflect"

	typed "github.com/sshaplygin/go-socket.io/internal/event"
)

type eventListener struct {
	handler *reflectedFunc
	once    bool
	fired   bool // protected by eventsLock
}

func (nh *namespaceHandler) OnEvent(event string, f interface{}) {
	listener := &eventListener{handler: newEventFunc(f)}
	nh.eventsLock.Lock()
	defer nh.eventsLock.Unlock()
	delete(nh.typed, event)
	nh.events[event] = []*eventListener{listener}
}

func (nh *namespaceHandler) AddListener(event string, f interface{}, once bool) func() {
	listener := &eventListener{handler: newEventFunc(f), once: once}
	nh.eventsLock.Lock()
	defer nh.eventsLock.Unlock()
	if _, exists := nh.typed[event]; exists {
		panic("typed and reflected event listeners cannot coexist")
	}
	current := nh.events[event]
	if len(current) > 0 && (!reflect.DeepEqual(current[0].handler.argTypes, listener.handler.argTypes) || current[0].handler.Variadic() != listener.handler.Variadic()) {
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
	delete(nh.typed, event)
}

func (nh *namespaceHandler) Listeners(event string) []*eventListener {
	nh.eventsLock.RLock()
	defer nh.eventsLock.RUnlock()
	return append([]*eventListener(nil), nh.events[event]...)
}

func (nh *namespaceHandler) DispatchEvent(conn Conn, event string, args ...reflect.Value) ([]reflect.Value, error) {
	ret, _, err := nh.DispatchListeners(conn, event, nh.Listeners(event), func(...interface{}) {}, args...)
	return ret, err
}

func (nh *namespaceHandler) DispatchListeners(conn Conn, event string, snapshot []*eventListener, ack AckFunc, args ...reflect.Value) ([]reflect.Value, bool, error) {
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

func (l *eventListener) ArgTypes() []reflect.Type { return l.handler.argTypes }

func (l *eventListener) Variadic() bool { return l.handler.Variadic() }

// RegisterEvent atomically reserves one typed schema against reflected listeners.
func (nh *namespaceHandler) RegisterEvent(name string, handler typed.Handler) error {
	if err := typed.ValidateName(name); err != nil {
		return err
	}
	if !handler.Valid() {
		return typed.ErrInvalidPayload
	}
	nh.eventsLock.Lock()
	defer nh.eventsLock.Unlock()
	if len(nh.events[name]) != 0 {
		return typed.ErrDuplicateEvent
	}
	if _, exists := nh.typed[name]; exists {
		return typed.ErrDuplicateEvent
	}
	if nh.typed == nil {
		nh.typed = make(map[string]typed.Handler)
	}
	nh.typed[name] = handler
	return nil
}
func (nh *namespaceHandler) EventSnapshot(name string) ([]*eventListener, typed.Handler, bool) {
	nh.eventsLock.RLock()
	defer nh.eventsLock.RUnlock()
	handler, exists := nh.typed[name]
	return append([]*eventListener(nil), nh.events[name]...), handler, exists
}
