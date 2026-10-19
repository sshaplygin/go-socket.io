package connection

import (
	"context"
	"errors"
	"reflect"

	eventImpl "github.com/sshaplygin/go-socket.io/internal/event"
	"github.com/sshaplygin/go-socket.io/parser"
)

type typedResult struct {
	payload eventImpl.Payload
	err     error
}
type typedPending struct {
	schema eventImpl.Schema
	result chan typedResult
}

// endTyped cancels this namespace generation before any disconnect callbacks.
// It invokes no application callback and is safe while the connection lock is held.
func (nc *namespaceConn) endTyped() {
	if nc.cancelLifetime != nil {
		nc.cancelLifetime()
	}
	nc.ack.Range(func(id, value interface{}) bool {
		if pending, ok := value.(*typedPending); ok {
			nc.completeTyped(id.(uint64), pending, eventImpl.Payload{}, eventImpl.ErrSocketClosed)
		}
		return true
	})
}

func (nc *namespaceConn) completeTyped(id uint64, pending *typedPending, payload eventImpl.Payload, err error) bool {
	if !nc.ack.CompareAndDelete(id, pending) {
		return false
	}
	pending.result <- typedResult{payload: payload, err: err}
	return true
}

// SendEvent is the checked, unary typed sending capability.
func (nc *namespaceConn) SendEvent(ctx context.Context, name string, payload eventImpl.Payload) error {
	_, err := nc.admitTyped(eventImpl.Context(ctx), name, payload, nil)
	return err
}

// RequestAck binds one request to this concrete namespace generation.
func (nc *namespaceConn) RequestAck(ctx context.Context, name string, payload eventImpl.Payload, schema eventImpl.Schema) (eventImpl.Payload, error) {
	ctx = eventImpl.Context(ctx)
	wait, cancel := context.WithTimeout(ctx, nc.ackTimeout)
	defer cancel()
	pending := &typedPending{schema: schema, result: make(chan typedResult, 1)}
	id, err := nc.admitTyped(wait, name, payload, pending)
	if err != nil {
		return eventImpl.Payload{}, ackContextError(ctx, wait, err)
	}
	select {
	case result := <-pending.result:
		return result.payload, result.err
	case <-wait.Done():
		err := ackContextError(ctx, wait, wait.Err())
		nc.completeTyped(id, pending, eventImpl.Payload{}, err)
		result := <-pending.result
		return result.payload, result.err
	}
}

func ackContextError(caller, wait context.Context, err error) error {
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if caller.Err() != nil {
		return caller.Err()
	}
	if wait.Err() != nil {
		return eventImpl.ErrAckTimeout
	}
	return err
}

func (nc *namespaceConn) admitTyped(ctx context.Context, name string, payload eventImpl.Payload, pending *typedPending) (uint64, error) {
	if err := eventImpl.ValidateName(name); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if !payload.Valid() {
		return 0, eventImpl.ErrInvalidPayload
	}
	header := parser.Header{Type: parser.Event, Namespace: fmtNS(nc.namespace)}
	pkg := preparePayload(header, false, reflect.ValueOf(name), reflect.ValueOf(payload.Interface()))
	if pkg.err != nil {
		return 0, pkg.err
	}
	nc.outMu.Lock()
	defer nc.outMu.Unlock()
	c := nc.Connection
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	current, exists := c.namespaces.Get(fmtNS(nc.namespace))
	if nc.rejected || !exists || current != nc || isDone(c.closing) || isDone(c.engineDone) || nc.lifetime.Err() != nil {
		return 0, eventImpl.ErrSocketClosed
	}
	if c.pendingWrites+len(c.writeChan)+c.drainQueued >= cap(c.writeChan) {
		c.overflowWrite(header)
		return 0, ErrWriteBufferFull
	}
	if pending != nil {
		header.NeedAck = true
		header.ID = c.nextID()
		nc.ack.Store(header.ID, pending)
		pkg.Header = header
		pkg.prepared = pkg.prepared.WithHeader(header)
	}
	if nc.connecting {
		c.pendingWrites++
		nc.pending = append(nc.pending, pkg)
	} else {
		c.queuePayloadLocked(pkg, false)
	}
	return header.ID, nil
}

var _ eventImpl.TypedEndpoint = (*namespaceConn)(nil)

// observeEngineClose only signals cancellation. The dispatcher retains ownership
// of disconnect callbacks and packet decoding, including while a handler runs.
func (c *Connection) observeEngineClose() {
	defer close(c.engineObserverDone)
	select {
	case <-c.done:
		return
	case <-c.engineDone:
	}
	c.mu.Lock()
	c.namespaces.Range(func(_ string, nc *namespaceConn) { nc.endTyped() })
	c.mu.Unlock()
}
