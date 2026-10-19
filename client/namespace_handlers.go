package client

import (
	"fmt"
	"github.com/sshaplygin/go-socket.io/internal/event"
	"reflect"
	"sync"
)

type namespaceHandler struct {
	events       map[string][]*eventListener
	typed        map[string]event.Handler
	eventsLock   sync.RWMutex
	onConnect    func(Conn) error
	onDisconnect func(Conn, string)
	onError      func(Conn, error)
}

func newNamespaceHandler(string) *namespaceHandler {
	return &namespaceHandler{events: make(map[string][]*eventListener)}
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

func (nh *namespaceHandler) ConnectHandler() func(Conn) error {
	nh.eventsLock.RLock()
	defer nh.eventsLock.RUnlock()
	f := nh.onConnect
	if f == nil {
		return nil
	}
	return func(conn Conn) (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				if cause, ok := recovered.(error); ok {
					err = fmt.Errorf("connect callback: %w", cause)
				} else {
					err = fmt.Errorf("connect callback: %v", recovered)
				}
			}
		}()
		return f(conn)
	}
}

func (nh *namespaceHandler) DisconnectHandler() func(Conn, string) {
	nh.eventsLock.RLock()
	defer nh.eventsLock.RUnlock()
	return nh.onDisconnect
}

func (nh *namespaceHandler) ErrorHandler() func(Conn, error) {
	nh.eventsLock.RLock()
	defer nh.eventsLock.RUnlock()
	return nh.onError
}

type reflectedFunc struct {
	argTypes []reflect.Type
	f        reflect.Value
	ack      bool
}

func (h *reflectedFunc) Call(args []reflect.Value) (ret []reflect.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			var ok bool
			err, ok = r.(error)
			if !ok {
				err = fmt.Errorf("event call error: %s", r)
			}
		}
	}()

	if h.f.Type().IsVariadic() && len(args) == h.f.Type().NumIn() && args[len(args)-1].Type() == h.f.Type().In(h.f.Type().NumIn()-1) {
		ret = h.f.CallSlice(args)
	} else {
		ret = h.f.Call(args)
	}

	return
}

func newEventFunc(f interface{}) *reflectedFunc {
	fv := reflect.ValueOf(f)

	if fv.Kind() != reflect.Func {
		panic("event handler must be a func.")
	}
	ft := fv.Type()

	if ft.NumIn() < 1 || ft.In(0) != reflect.TypeOf((*Conn)(nil)).Elem() {
		panic("handler function should be like func(socketio.Conn, ...)")
	}

	hasAck := ft.NumIn() > 1 && ft.In(ft.NumIn()-1) == reflect.TypeOf(AckFunc(nil))
	count := ft.NumIn() - 1
	if hasAck {
		count--
	}
	argTypes := make([]reflect.Type, count)
	for i := range argTypes {
		argTypes[i] = ft.In(i + 1)
	}

	if len(argTypes) == 0 {
		argTypes = nil
	}

	return &reflectedFunc{
		argTypes: argTypes,
		f:        fv,
		ack:      hasAck,
	}
}

func newAckFunc(f interface{}) *reflectedFunc {
	fv := reflect.ValueOf(f)

	if fv.Kind() != reflect.Func {
		panic("ack callback must be a func.")
	}

	ft := fv.Type()
	argTypes := make([]reflect.Type, ft.NumIn())

	for i := range argTypes {
		argTypes[i] = ft.In(i)
	}
	if len(argTypes) == 0 {
		argTypes = nil
	}

	return &reflectedFunc{
		argTypes: argTypes,
		f:        fv,
	}
}

// Rooms owns local client room membership.

// ArgTypes supplies the connection decoder with callback parameter types.
func (h *reflectedFunc) ArgTypes() []reflect.Type { return h.argTypes }

// Variadic reports whether the callback accepts a variadic final argument.
func (h *reflectedFunc) Variadic() bool { return h.f.Type().IsVariadic() }
