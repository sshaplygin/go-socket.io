package namespace

import (
	"fmt"
	"reflect"
	"sync"
)

type Handler struct {
	broadcast    *RoomIndex
	events       map[string][]*Listener
	eventsLock   sync.RWMutex
	onConnect    func(Conn) error
	onDisconnect func(Conn, string)
	onError      func(Conn, error)
}

func NewHandler(string) *Handler {
	return &Handler{broadcast: NewRoomIndex(), events: make(map[string][]*Listener)}
}

func (nh *Handler) OnConnect(f func(Conn) error) {
	nh.eventsLock.Lock()
	defer nh.eventsLock.Unlock()
	nh.onConnect = f
}

func (nh *Handler) OnDisconnect(f func(Conn, string)) {
	nh.eventsLock.Lock()
	defer nh.eventsLock.Unlock()
	nh.onDisconnect = f
}

func (nh *Handler) OnError(f func(Conn, error)) {
	nh.eventsLock.Lock()
	defer nh.eventsLock.Unlock()
	nh.onError = f
}

func (nh *Handler) ConnectHandler() func(Conn) error {
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

func (nh *Handler) DisconnectHandler() func(Conn, string) {
	nh.eventsLock.RLock()
	defer nh.eventsLock.RUnlock()
	return nh.onDisconnect
}

func (nh *Handler) ErrorHandler() func(Conn, error) {
	nh.eventsLock.RLock()
	defer nh.eventsLock.RUnlock()
	return nh.onError
}

type Func struct {
	argTypes []reflect.Type
	f        reflect.Value
	ack      bool
}

func (h *Func) Call(args []reflect.Value) (ret []reflect.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			var ok bool
			err, ok = r.(error)
			if !ok {
				err = fmt.Errorf("event call error: %s", r)
			}
		}
	}()

	ret = h.f.Call(args)

	return
}

func NewEventFunc(f interface{}) *Func {
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

	return &Func{
		argTypes: argTypes,
		f:        fv,
		ack:      hasAck,
	}
}

func NewAckFunc(f interface{}) *Func {
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

	return &Func{
		argTypes: argTypes,
		f:        fv,
	}
}

// Rooms owns local client room membership.
func (nh *Handler) Rooms() *RoomIndex { return nh.broadcast }

// ArgTypes supplies the connection decoder with callback parameter types.
func (h *Func) ArgTypes() []reflect.Type { return h.argTypes }
