package socketio

import (
	eventImpl "github.com/sshaplygin/go-socket.io/internal/event"
	"reflect"
)

type eventListener struct {
	handler *callbackFunc
	typed   *eventImpl.Handler
	once    bool
	fired   bool // protected by eventsLock; snapshots share this claim
}

func (nh *namespaceHandler) OnEvent(event string, f interface{}) {
	listener := &eventListener{handler: newEventFunc(f)}
	nh.eventsLock.Lock()
	defer nh.eventsLock.Unlock()
	nh.events[event] = []*eventListener{listener}
}

func (nh *namespaceHandler) AddListener(event string, f interface{}, once bool) func() {
	listener := &eventListener{handler: newEventFunc(f), once: once}
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

func (nh *namespaceHandler) Listeners(event string) []*eventListener {
	nh.eventsLock.RLock()
	defer nh.eventsLock.RUnlock()
	return append([]*eventListener(nil), nh.events[event]...)
}

func (nh *namespaceHandler) DispatchEvent(conn Conn, event string, report func(error), args ...reflect.Value) ([]reflect.Value, error) {
	ret, _, err := nh.DispatchListeners(conn, event, nh.Listeners(event), func(...interface{}) {}, report, args...)
	return ret, err
}

var errorType = reflect.TypeOf((*error)(nil)).Elem()

// DispatchListeners calls the listeners in order until one panics; that panic is
// the returned error. A non-nil result of type error is passed to report, if set,
// and replaced in the results by its error-first acknowledgement object; a typed
// nil counts as nil and an error whose methods panic as that recovered panic (see
// eventImpl.Returned).
func (nh *namespaceHandler) DispatchListeners(conn Conn, event string, snapshot []*eventListener, ack AckFunc, report func(error), args ...reflect.Value) ([]reflect.Value, bool, error) {
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
		for i, value := range ret {
			if value.Type() != errorType {
				continue
			}
			returned, _ := value.Interface().(error)
			returned = eventImpl.Returned(returned)
			if returned == nil {
				ret[i] = reflect.Zero(errorType) // a typed nil is sent as null
				continue
			}
			if report != nil {
				report(returned)
			}
			ret[i] = reflect.ValueOf(eventImpl.AckError(returned))
		}
		if len(result) == 0 && len(ret) != 0 {
			result = ret
		}
	}
	return result, ownsAck, nil
}

func (l *eventListener) ArgTypes() []reflect.Type {
	if l.handler == nil {
		return nil
	}
	return l.handler.argTypes
}

func (l *eventListener) Variadic() bool { return l.handler != nil && l.handler.Variadic() }

// RegisterEvent installs a typed handler only if the event has no registration.
func (nh *namespaceHandler) RegisterEvent(name string, handler eventImpl.Handler) error {
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
	nh.events[name] = []*eventListener{{typed: &handler}}
	return nil
}

func (l *eventListener) TypedHandler() (eventImpl.Handler, bool) {
	if l.typed == nil {
		return eventImpl.Handler{}, false
	}
	return *l.typed, true
}
