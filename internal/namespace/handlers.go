package namespace

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/sshaplygin/go-socket.io/adapter"
)

type Handler struct {
	connectedMu sync.RWMutex
	connected   map[string]Conn
	middleware  []func(Conn) error
	broadcast   adapter.Adapter
	err         error // why broadcast is a NopAdapter: the adapter could not be created

	events     map[string][]*Listener
	eventsLock sync.RWMutex

	onConnect    func(conn Conn) error
	onDisconnect func(conn Conn, msg string)
	onError      func(conn Conn, err error)
}

func NewHandler() *Handler {
	return &Handler{broadcast: adapter.NewMemory(), connected: make(map[string]Conn), events: make(map[string][]*Listener)}
}

// NopAdapter is the adapter.Adapter of a namespace whose adapter could not be
// created: it keeps no rooms and sends nothing.
type NopAdapter struct{}

func (NopAdapter) Join(string, adapter.Socket) {}

func (NopAdapter) Leave(string, adapter.Socket) {}

func (NopAdapter) LeaveAll(adapter.Socket) {}

func (NopAdapter) Clear(string) {}

func (NopAdapter) Send(string, string, ...interface{}) {}

func (NopAdapter) SendAll(string, ...interface{}) {}

func (NopAdapter) ForEach(string, adapter.EachFunc) {}

func (NopAdapter) Len(string) int { return -1 }

func (NopAdapter) Rooms(adapter.Socket) []string { return nil }

func (NopAdapter) AllRooms() []string { return nil }

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

func (nh *Handler) Connect(c Conn) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("connect callback panic: %v", value)
		}
	}()
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

func (nh *Handler) AddSocket(c Conn) {
	nh.connectedMu.Lock()
	nh.connected[c.ID()] = c
	nh.connectedMu.Unlock()
}

func (nh *Handler) RemoveSocket(c Conn) {
	nh.connectedMu.Lock()
	if nh.connected[c.ID()] == c {
		delete(nh.connected, c.ID())
	}
	nh.connectedMu.Unlock()
}

func (NopAdapter) SendTo(adapter.Options, string, ...interface{}) error { return nil }

func (NopAdapter) IDs(...string) []string { return nil }

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

	if h.f.Type().IsVariadic() && len(args) == h.f.Type().NumIn() && args[len(args)-1].IsValid() && args[len(args)-1].Type() == h.f.Type().In(len(args)-1) {
		ret = h.f.CallSlice(args)
	} else {
		ret = h.f.Call(args)
	}

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

// SetAdapter installs the resolved adapter and its construction error before registration.
func (nh *Handler) SetAdapter(b adapter.Adapter, err error) { nh.broadcast, nh.err = b, err }

func (nh *Handler) Adapter() adapter.Adapter { return nh.broadcast }
func (nh *Handler) Err() error               { return nh.err }

// ErrorHandler snapshots the callback; callers invoke it outside registry locks.
func (nh *Handler) ErrorHandler() func(Conn, error) {
	nh.eventsLock.RLock()
	defer nh.eventsLock.RUnlock()
	callback := nh.onError
	if callback == nil {
		return nil
	}
	return func(c Conn, err error) {
		defer func() { _ = recover() }()
		callback(c, err)
	}
}

func (nh *Handler) NotifyDisconnect(c Conn, reason string) {
	defer func() {
		if value := recover(); value != nil {
			if callback := nh.ErrorHandler(); callback != nil {
				callback(c, fmt.Errorf("disconnect callback panic: %v", value))
			}
		}
	}()
	nh.eventsLock.RLock()
	callback := nh.onDisconnect
	nh.eventsLock.RUnlock()
	if callback != nil {
		callback(c, reason)
	}
}

func (nh *Handler) Use(f func(Conn) error) {
	if f == nil {
		return
	}
	nh.eventsLock.Lock()
	nh.middleware = append(nh.middleware, f)
	nh.eventsLock.Unlock()
}

func (nh *Handler) Sockets() []Conn {
	nh.connectedMu.RLock()
	defer nh.connectedMu.RUnlock()
	result := make([]Conn, 0, len(nh.connected))
	for _, c := range nh.connected {
		result = append(result, c)
	}
	return result
}

// ArgTypes returns the parameter types used by the connection decoder.
func (h *Func) ArgTypes() []reflect.Type { return h.argTypes }

// Variadic reports whether the final payload type consumes remaining arguments.
func (h *Func) Variadic() bool { return h.f.Type().IsVariadic() }
