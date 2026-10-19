package namespace

import (
	"errors"
	eventImpl "github.com/sshaplygin/go-socket.io/internal/event"
	"reflect"

	"github.com/sshaplygin/go-socket.io/parser"
)

type Listener struct {
	handler *Func
	typed   *eventImpl.Handler
	once    bool
	fired   bool // protected by eventsLock; snapshots share this claim
}

func (nh *Handler) OnEvent(event string, f interface{}) {
	listener := &Listener{handler: NewEventFunc(f)}
	nh.eventsLock.Lock()
	defer nh.eventsLock.Unlock()
	nh.events[event] = []*Listener{listener}
}

func (nh *Handler) AddListener(event string, f interface{}, once bool) func() {
	listener := &Listener{handler: NewEventFunc(f), once: once}
	nh.eventsLock.Lock()
	defer nh.eventsLock.Unlock()
	current := nh.events[event]
	if len(current) > 0 && (current[0].typed != nil || !reflect.DeepEqual(current[0].handler.argTypes, listener.handler.argTypes) || current[0].handler.Variadic() != listener.handler.Variadic()) {
		panic("event listeners must have identical argument types")
	}
	nh.events[event] = append(current, listener)
	return func() {
		nh.eventsLock.Lock()
		defer nh.eventsLock.Unlock()
		nh.removeListener(event, listener)
	}
}

func (nh *Handler) removeListener(event string, target *Listener) {
	current := nh.events[event]
	for i, listener := range current {
		if listener == target {
			next := append([]*Listener(nil), current[:i]...)
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

func (nh *Handler) Off(event string) {
	nh.eventsLock.Lock()
	defer nh.eventsLock.Unlock()
	delete(nh.events, event)
}

func (nh *Handler) Listeners(event string) []*Listener {
	nh.eventsLock.RLock()
	defer nh.eventsLock.RUnlock()
	return append([]*Listener(nil), nh.events[event]...)
}

func (nh *Handler) getEventTypes(event string) []reflect.Type {
	listeners := nh.Listeners(event)
	if len(listeners) != 0 {
		return listeners[0].ArgTypes()
	}
	return nil
}

func (nh *Handler) dispatch(conn Conn, header parser.Header, args ...reflect.Value) ([]reflect.Value, error) {
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

func (nh *Handler) DispatchEvent(conn Conn, event string, args ...reflect.Value) ([]reflect.Value, error) {
	ret, _, err := nh.DispatchListeners(conn, event, nh.Listeners(event), func(...interface{}) {}, args...)
	return ret, err
}

func (nh *Handler) DispatchListeners(conn Conn, event string, snapshot []*Listener, ack AckFunc, args ...reflect.Value) ([]reflect.Value, bool, error) {
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

func (l *Listener) ArgTypes() []reflect.Type {
	if l.handler == nil {
		return nil
	}
	return l.handler.argTypes
}

func (l *Listener) Variadic() bool { return l.handler != nil && l.handler.Variadic() }

// RegisterEvent installs a typed handler only if the event has no registration.
func (nh *Handler) RegisterEvent(name string, handler eventImpl.Handler) error {
	if err := eventImpl.ValidateName(name); err != nil {
		return err
	}
	if !handler.Valid() {
		return eventImpl.ErrInvalidPayload
	}
	nh.eventsLock.Lock()
	defer nh.eventsLock.Unlock()
	if len(nh.events[name]) != 0 {
		return eventImpl.ErrDuplicateEvent
	}
	nh.events[name] = []*Listener{{typed: &handler}}
	return nil
}

func (l *Listener) TypedHandler() (eventImpl.Handler, bool) {
	if l.typed == nil {
		return eventImpl.Handler{}, false
	}
	return *l.typed, true
}
