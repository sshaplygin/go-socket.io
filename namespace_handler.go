package socketio

import (
	"cmp"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/sshaplygin/go-socket.io/parser"
)

type namespaceHandler struct {
	connectedMu sync.RWMutex
	connected   map[string]Conn
	view        *ServerNamespace
	middleware  []func(Conn) error
	broadcast   Broadcast
	err         error // why broadcast is a nopBroadcast: the Redis broadcast could not be created

	events     map[string]*funcHandler
	eventsLock sync.RWMutex

	onConnect    func(conn Conn) error
	onDisconnect func(conn Conn, msg string)
	onError      func(conn Conn, err error)
}

func newNamespaceHandler(nsp string, adapterOpts *RedisAdapterOptions) *namespaceHandler {
	nh := &namespaceHandler{
		broadcast: nopBroadcast{},
		connected: make(map[string]Conn),
		events:    make(map[string]*funcHandler),
	}
	if adapterOpts == nil {
		nh.broadcast = newBroadcast()
	} else if bc, err := newRedisBroadcast(nsp, adapterOpts); err != nil {
		nh.err = fmt.Errorf("socketio: redis broadcast of namespace %q: %w", cmp.Or(nsp, aliasRootNamespace), err)
	} else {
		nh.broadcast = bc
	}
	return nh
}

// nopBroadcast is the Broadcast of a namespace whose Redis broadcast could not be
// created: it keeps no rooms and sends nothing.
type nopBroadcast struct{}

func (nopBroadcast) Join(string, Conn)                   {}
func (nopBroadcast) Leave(string, Conn)                  {}
func (nopBroadcast) LeaveAll(Conn)                       {}
func (nopBroadcast) Clear(string)                        {}
func (nopBroadcast) Send(string, string, ...interface{}) {}
func (nopBroadcast) SendAll(string, ...interface{})      {}
func (nopBroadcast) ForEach(string, EachFunc)            {}
func (nopBroadcast) Len(string) int                      { return -1 }
func (nopBroadcast) Rooms(Conn) []string                 { return nil }
func (nopBroadcast) AllRooms() []string                  { return nil }

func (nh *namespaceHandler) OnConnect(f func(Conn) error) {
	nh.onConnect = f
}

func (nh *namespaceHandler) OnDisconnect(f func(Conn, string)) {
	nh.onDisconnect = f
}

func (nh *namespaceHandler) OnError(f func(Conn, error)) {
	nh.onError = f
}

func (nh *namespaceHandler) OnEvent(event string, f interface{}) {
	nh.eventsLock.Lock()
	defer nh.eventsLock.Unlock()

	nh.events[event] = newEventFunc(f)
}

func (nh *namespaceHandler) getEventTypes(event string) []reflect.Type {
	nh.eventsLock.RLock()
	namespaceHandler := nh.events[event]
	nh.eventsLock.RUnlock()

	if namespaceHandler != nil {
		return namespaceHandler.argTypes
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
	nh.eventsLock.RLock()
	namespaceHandler := nh.events[event]
	nh.eventsLock.RUnlock()

	if namespaceHandler == nil {
		return nil, nil
	}

	return namespaceHandler.Call(append([]reflect.Value{reflect.ValueOf(conn)}, args...))
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
	nh.eventsLock.RUnlock()
	for _, f := range middleware {
		if err := f(c); err != nil {
			return err
		}
	}
	if nh.onConnect != nil {
		return nh.onConnect(c)
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

func (nopBroadcast) SendTo(BroadcastOptions, string, ...interface{}) error { return nil }
func (nopBroadcast) IDs(...string) []string                                { return nil }
