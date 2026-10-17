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

type eventListener struct {
	handler *funcHandler
}

func (nh *namespaceHandler) OnEvent(event string, f interface{}) {
	listener := &eventListener{handler: newEventFunc(f)}
	nh.eventsLock.Lock()
	defer nh.eventsLock.Unlock()
	nh.events[event] = []*eventListener{listener}
}

func (nh *namespaceHandler) listeners(event string) []*eventListener {
	nh.eventsLock.RLock()
	defer nh.eventsLock.RUnlock()
	return append([]*eventListener(nil), nh.events[event]...)
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
