package socketio

import (
	"fmt"
	"log/slog"
	"reflect"
	"sync"

	"github.com/sshaplygin/go-socket.io/adapter"
	"github.com/sshaplygin/go-socket.io/logger"
)

type namespaceHandler struct {
	name        string       // installed before registration
	log         *slog.Logger // installed before registration
	connectedMu sync.RWMutex
	connected   map[string]Conn
	middleware  []func(Conn) error
	broadcast   adapter.Adapter
	err         error // why broadcast is unavailable: the adapter could not be created

	events     map[string][]*eventListener
	eventsLock sync.RWMutex

	onConnect    func(conn Conn) error
	onDisconnect func(conn Conn, msg string)
	onError      func(conn Conn, err error)
}

func newNamespaceHandler() *namespaceHandler {
	return &namespaceHandler{log: logger.Log, broadcast: adapter.NewMemory(), connected: make(map[string]Conn), events: make(map[string][]*eventListener)}
}

// unavailableAdapter is the adapter.Adapter of a namespace whose adapter could not be
// created: it keeps no rooms, sends nothing and reports the construction error.
type unavailableAdapter struct{ err error }

func (unavailableAdapter) Join(string, adapter.Socket) {}

func (unavailableAdapter) Leave(string, adapter.Socket) {}

func (unavailableAdapter) LeaveAll(adapter.Socket) {}

func (unavailableAdapter) Clear(string) {}

func (unavailableAdapter) Send(string, string, ...interface{}) {}

func (unavailableAdapter) SendAll(string, ...interface{}) {}

func (unavailableAdapter) ForEach(string, adapter.EachFunc) {}

func (unavailableAdapter) Len(string) int { return -1 }

func (unavailableAdapter) Rooms(adapter.Socket) []string { return nil }

func (unavailableAdapter) AllRooms() []string { return nil }

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

func (nh *namespaceHandler) Connect(c Conn) (err error) {
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

func (nh *namespaceHandler) AddSocket(c Conn) {
	nh.connectedMu.Lock()
	nh.connected[c.ID()] = c
	nh.connectedMu.Unlock()
}

func (nh *namespaceHandler) RemoveSocket(c Conn) {
	nh.connectedMu.Lock()
	if nh.connected[c.ID()] == c {
		delete(nh.connected, c.ID())
	}
	nh.connectedMu.Unlock()
}

func (b unavailableAdapter) SendTo(adapter.Options, string, ...interface{}) error { return b.err }

func (unavailableAdapter) IDs(...string) []string { return nil }

type callbackFunc struct {
	argTypes []reflect.Type
	f        reflect.Value
	ack      bool
}

func (h *callbackFunc) Call(args []reflect.Value) (ret []reflect.Value, err error) {
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

func newEventFunc(f interface{}) *callbackFunc {
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

	return &callbackFunc{
		argTypes: argTypes,
		f:        fv,
		ack:      hasAck,
	}
}

func newAckFunc(f interface{}) *callbackFunc {
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

	return &callbackFunc{
		argTypes: argTypes,
		f:        fv,
	}
}

// SetAdapter installs the resolved adapter and its construction error before registration.
func (nh *namespaceHandler) SetAdapter(b adapter.Adapter, err error) {
	if err != nil {
		b = unavailableAdapter{err: err}
	}
	nh.broadcast, nh.err = b, err
	if binding, ok := b.(adapter.SocketLookupBinder); ok && err == nil {
		binding.BindSocketLookup(func(id string) adapter.Socket {
			nh.connectedMu.RLock()
			defer nh.connectedMu.RUnlock()
			socket, _ := nh.connected[id].(adapter.Socket)
			return socket
		})
	}
}

func (nh *namespaceHandler) Adapter() adapter.Adapter { return nh.broadcast }
func (nh *namespaceHandler) Err() error               { return nh.err }

// ErrorHandler snapshots the callback; callers invoke it outside registry locks.
func (nh *namespaceHandler) ErrorHandler() func(Conn, error) {
	nh.eventsLock.RLock()
	defer nh.eventsLock.RUnlock()
	callback := nh.onError
	if callback == nil {
		return nil
	}
	return func(c Conn, err error) {
		defer func() {
			if value := recover(); value != nil {
				nh.log.Warn("socketio: error handler panic", nspAttr(nh.name), "err", fmt.Errorf("%v", value))
			}
		}()
		callback(c, err)
	}
}

func (nh *namespaceHandler) NotifyDisconnect(c Conn, reason string) {
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

func (nh *namespaceHandler) Use(f func(Conn) error) {
	if f == nil {
		return
	}
	nh.eventsLock.Lock()
	nh.middleware = append(nh.middleware, f)
	nh.eventsLock.Unlock()
}

func (nh *namespaceHandler) Sockets() []Conn {
	nh.connectedMu.RLock()
	defer nh.connectedMu.RUnlock()
	result := make([]Conn, 0, len(nh.connected))
	for _, c := range nh.connected {
		result = append(result, c)
	}
	return result
}

// ArgTypes returns the parameter types used by the connection decoder.
func (h *callbackFunc) ArgTypes() []reflect.Type { return h.argTypes }

// Variadic reports whether the final payload type consumes remaining arguments.
func (h *callbackFunc) Variadic() bool { return h.f.Type().IsVariadic() }
