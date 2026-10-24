package client

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/sshaplygin/go-socket.io/internal/event"
	"github.com/sshaplygin/go-socket.io/parser"
)

type typedResult struct {
	value event.Payload
	err   error
}

type typedRequest struct {
	schema    event.Schema
	done      chan typedResult
	once      sync.Once
	finished  atomic.Bool
	mu        sync.Mutex
	namespace *namespaceConn
	id        uint64
}

func newTypedRequest(schema event.Schema) *typedRequest {
	return &typedRequest{schema: schema, done: make(chan typedResult, 1)}
}
func (r *typedRequest) complete(value event.Payload, err error) {
	r.once.Do(func() { r.finished.Store(true); r.done <- typedResult{value, err} })
}
func (r *typedRequest) remove() {
	r.mu.Lock()
	nc, id := r.namespace, r.id
	r.mu.Unlock()
	if nc != nil {
		nc.conn.mu.Lock()
		if nc.typedAcks[id] == r {
			delete(nc.typedAcks, id)
		}
		nc.conn.mu.Unlock()
	}
}
func (r *typedRequest) await(ctx context.Context) (event.Payload, error) {
	var result typedResult
	select {
	case result = <-r.done:
	case <-ctx.Done():
		r.complete(event.Payload{}, ctx.Err())
		result = <-r.done
	}
	r.remove()
	return result.value, result.err
}

type typedEmission struct {
	ctx     context.Context
	request *typedRequest
	stop    func() bool
}

// cancelTypedLocked ends this generation before application disconnect callbacks.
// conn.mu is held; completion only signals a buffered result, never user code.
func (nc *namespaceConn) cancelTypedLocked() {
	if nc.cancelLifetime != nil {
		nc.cancelLifetime()
	}
	for id, request := range nc.typedAcks {
		delete(nc.typedAcks, id)
		request.complete(event.Payload{}, event.ErrSocketClosed)
	}
}
func (nc *namespaceConn) endTyped()                       { nc.conn.mu.Lock(); nc.cancelTypedLocked(); nc.conn.mu.Unlock() }
func (nc *namespaceConn) handlerContext() context.Context { return event.Context(nc.lifetime) }

func typedPacket(namespace, name string, value event.Payload) (*parser.PreparedPacket, error) {
	if err := event.ValidateName(name); err != nil {
		return nil, err
	}
	if !value.Valid() {
		return nil, event.ErrInvalidPayload
	}
	return parser.Prepare(parser.Header{Type: parser.Event, Namespace: fmtNS(namespace)}, []interface{}{name, value.Interface()})
}

// admitTyped preserves the original generation even when its manager reconnects.
func (nc *namespaceConn) admitTyped(ctx context.Context, packet *parser.PreparedPacket, request *typedRequest) error {
	nc.outMu.Lock()
	defer nc.outMu.Unlock()
	c := nc.conn
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	current, exists := c.namespaces.Get(fmtNS(nc.namespace))
	if !exists || current != nc || isDone(c.closing) || isDone(c.engineDone()) || nc.handlerContext().Err() != nil {
		return event.ErrSocketClosed
	}
	if request != nil && request.finished.Load() {
		return event.ErrSocketClosed
	}
	if len(c.writeChan) >= cap(c.writeChan) {
		c.overflowWrite(parser.Header{Namespace: fmtNS(nc.namespace)})
		return ErrWriteBufferFull
	}
	header := parser.Header{Type: parser.Event, Namespace: fmtNS(nc.namespace)}
	if request != nil {
		header.ID = c.nextID()
		header.NeedAck = true
		if nc.typedAcks == nil {
			nc.typedAcks = make(map[uint64]*typedRequest)
		}
		nc.typedAcks[header.ID] = request
		request.mu.Lock()
		request.namespace, request.id = nc, header.ID
		request.mu.Unlock()
	}
	c.queuePreparedLocked(header, false, packet.WithHeader(header), nil)
	return nil
}

func (nc *namespaceConn) SendEvent(ctx context.Context, name string, value event.Payload) error {
	ctx = event.Context(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	packet, err := typedPacket(nc.namespace, name, value)
	if err != nil {
		return err
	}
	return nc.admitTyped(ctx, packet, nil)
}
func (nc *namespaceConn) RequestAck(ctx context.Context, name string, value event.Payload, schema event.Schema) (event.Payload, error) {
	ctx = event.Context(ctx)
	if err := ctx.Err(); err != nil {
		return event.Payload{}, err
	}
	packet, err := typedPacket(nc.namespace, name, value)
	if err != nil {
		return event.Payload{}, err
	}
	request := newTypedRequest(schema)
	if err := nc.admitTyped(ctx, packet, request); err != nil {
		return event.Payload{}, err
	}
	return request.await(ctx)
}

// RegisterEvent is an internal bridge for socketio.Event and socketio.AckEvent
// descriptors only. Applications register typed handlers through those descriptors.
func (s *Socket) RegisterEvent(name string, handler event.Handler) error {
	return s.handler.RegisterEvent(name, handler)
}

// admitTyped buffers stable socket operations until namespace CONNECT, retaining
// their deadline. No acknowledgement ID belongs to a pre-connect operation.
func (s *Socket) admitTyped(ctx context.Context, packet *parser.PreparedPacket, request *typedRequest) (*typedEmission, error) {
	m := s.state
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !s.active || m.stopped {
		return nil, event.ErrSocketClosed
	}
	if s.connected && s.nc != nil {
		return nil, s.nc.admitTyped(ctx, packet, request)
	}
	if m.buffered >= m.client.limits.writeBufferSize {
		return nil, ErrWriteBufferFull
	}
	emission := &typedEmission{ctx: ctx, request: request}
	s.buffer = append(s.buffer, bufferedEvent{prepared: packet, typed: emission})
	m.buffered++
	emission.stop = context.AfterFunc(ctx, func() {
		s.removeTypedEmission(emission)
		if request != nil {
			request.complete(event.Payload{}, ctx.Err())
			request.remove()
		}
	})
	return emission, nil
}
func (s *Socket) removeTypedEmission(target *typedEmission) {
	m := s.state
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, record := range s.buffer {
		if record.typed == target {
			copy(s.buffer[i:], s.buffer[i+1:])
			s.buffer[len(s.buffer)-1] = bufferedEvent{}
			s.buffer = s.buffer[:len(s.buffer)-1]
			m.buffered--
			break
		}
	}
	if target.stop != nil {
		target.stop()
	}
}

// SendEvent is the internal bridge used by socketio.Event.Emit. Applications
// send typed values through the descriptor rather than calling this method.
func (s *Socket) SendEvent(ctx context.Context, name string, value event.Payload) error {
	ctx = event.Context(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	packet, err := typedPacket(s.namespace, name, value)
	if err != nil {
		return err
	}
	_, err = s.admitTyped(ctx, packet, nil)
	return err
}

// RequestAck is the internal bridge used by socketio.AckEvent.EmitWithAck.
// Applications request typed responses through the descriptor.
func (s *Socket) RequestAck(ctx context.Context, name string, value event.Payload, schema event.Schema) (event.Payload, error) {
	ctx = event.Context(ctx)
	if err := ctx.Err(); err != nil {
		return event.Payload{}, err
	}
	packet, err := typedPacket(s.namespace, name, value)
	if err != nil {
		return event.Payload{}, err
	}
	request := newTypedRequest(schema)
	emission, err := s.admitTyped(ctx, packet, request)
	if err != nil {
		return event.Payload{}, err
	}
	result, err := request.await(ctx)
	if emission != nil {
		s.removeTypedEmission(emission)
	}
	return result, err
}

// m.mu is held. Untyped buffered events remain available for a later Connect.
func (s *Socket) cancelBufferedTypedLocked() {
	kept := s.buffer[:0]
	for _, record := range s.buffer {
		if record.typed == nil {
			kept = append(kept, record)
			continue
		}
		m := record.typed
		if m.stop != nil {
			m.stop()
		}
		if m.request != nil {
			m.request.complete(event.Payload{}, event.ErrSocketClosed)
		}
		s.state.buffered--
	}
	for i := len(kept); i < len(s.buffer); i++ {
		s.buffer[i] = bufferedEvent{}
	}
	s.buffer = kept
}

func dispatchTyped(c *conn, nc *namespaceConn, header parser.Header, handler event.Handler) error {
	payload, err := handler.Decode(c.decoder)
	if err == nil {
		var reply event.Payload
		reply, err = handler.Invoke(nc.handlerContext(), nc, payload)
		if handler.WantsAck() && header.NeedAck {
			writeTypedAck(nc, header, event.AckArgs(reply, err))
		}
	} else if handler.WantsAck() && header.NeedAck {
		writeTypedAck(nc, header, event.AckArgs(event.Payload{}, &event.RemoteError{Code: "invalid_payload", Message: err.Error()}))
	}
	if err != nil {
		c.onError(header.Namespace, err)
	}
	return nil
}
func writeTypedAck(nc *namespaceConn, header parser.Header, args []interface{}) {
	header.Type = parser.Ack
	values := make([]reflect.Value, len(args))
	for i, value := range args {
		values[i] = reflect.ValueOf(value)
	}
	nc.writeAck(header, values...)
}

func (m *manager) RegisterEvent(name string, handler event.Handler) error {
	return m.Socket(m.namespace).RegisterEvent(name, handler)
}
func (m *manager) SendEvent(ctx context.Context, name string, value event.Payload) error {
	return m.Socket(m.namespace).SendEvent(ctx, name, value)
}
func (m *manager) RequestAck(ctx context.Context, name string, value event.Payload, schema event.Schema) (event.Payload, error) {
	return m.Socket(m.namespace).RequestAck(ctx, name, value, schema)
}

// engineDone is an optional capability of built-in engines. Custom transports
// keep the existing read/write failure lifecycle without another frame reader.
func (c *conn) engineDone() <-chan struct{} { return c.engineClose }

func (c *conn) observeEngineClose() {
	done := c.engineDone()
	if done == nil {
		return
	}
	select {
	case <-done:
		c.mu.Lock()
		c.namespaces.Range(func(_ string, nc *namespaceConn) { nc.cancelTypedLocked() })
		c.mu.Unlock()
	case <-c.done:
	}
}
