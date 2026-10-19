package client

import (
	"errors"
	"reflect"
	"time"

	engineclient "github.com/sshaplygin/go-socket.io/engineio/client"
)

func (c *conn) Close() error {
	c.mu.Lock()
	if isDone(c.closing) {
		c.mu.Unlock()
		return nil
	}
	ncs := c.startClose()
	c.draining, c.drainTimer = true, time.AfterFunc(c.drainTimeout, c.stop)
	c.mu.Unlock()
	c.disconnect(ncs)
	c.mu.Lock()
	c.appSealed = true
	close(c.seal)
	c.mu.Unlock()
	return nil
}

// startClose takes the connected namespaces; c.mu is held.
func (c *conn) startClose() (ncs []*namespaceConn) {
	close(c.closing)
	c.namespaces.Range(func(_ string, nc *namespaceConn) { ncs = append(ncs, nc) })
	for _, nc := range ncs {
		nc.draining = true
		nc.cancelTypedLocked()
		c.namespaces.Delete(fmtNS(nc.namespace))
	}
	c.disconnects.Add(len(ncs))
	c.disconnecting += len(ncs)
	return ncs
}

// stop starts a library close, or ends a drain.
func (c *conn) stop() { c.mu.Lock(); defer c.mu.Unlock(); c.stopLocked() }

func (c *conn) stopLocked() {
	if !isDone(c.closing) {
		c.pending = c.startClose()
	}
	if !isDone(c.discard) {
		close(c.discard)
		if c.drainTimer != nil {
			c.drainTimer.Stop()
		}
		go func() { _ = c.Conn.Close() }() // the emitter of an overflow must not block
	}
}

// finish ends the close: it discards, then runs the OnDisconnect calls a library close owes.
func (c *conn) finish() {
	c.mu.Lock()
	c.stopLocked()
	ncs := c.pending
	c.mu.Unlock()
	c.disconnect(ncs)
	close(c.done)
}

func (c *conn) disconnect(ncs []*namespaceConn) {
	c.mu.Lock()
	reason := c.disconnectReason
	c.mu.Unlock()
	for _, nc := range ncs {
		c.disconnectNamespace(nc, reason)

	}
}

// disconnectNamespace delivers disconnecting followed by OnDisconnect.
func (c *conn) disconnectNamespace(nc *namespaceConn, reason string) {
	c.mu.Lock()
	draining := c.draining
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.disconnecting--
		c.mu.Unlock()
		c.disconnects.Done()
	}()
	nh := c.namespace(fmtNS(nc.namespace))
	if c.owner != nil && !draining {
		c.owner.detach(nc)
	}
	if nh != nil {
		if _, err := nh.DispatchEvent(nc, "disconnecting", reflect.ValueOf(reason)); err != nil {
			if f := nh.ErrorHandler(); f != nil {
				f(nc, err)
			} else {
				c.unhandled(nc.namespace, err)
			}
		}
	}
	if nh != nil {
		if f := nh.DisconnectHandler(); f != nil {
			f(nc, reason)
		}
	}
	if c.owner != nil && draining {
		c.owner.detach(nc)
	}
}

// finishRead obtains the final engine cause on the read goroutine, after Decode
// returned and before finish invokes any OnDisconnect handlers.
func (c *conn) finishRead() {
	reason := "forced close"
	var closed *engineclient.CloseError
	failed := c.reader.failed
	if failed != nil && !errors.As(failed, &closed) {
		// Dispatch has ended and its error was already reported. Start the
		// close before the engine shuts down, so the writer cannot report that
		// shutdown as a second failure while the reader obtains its cause.
		c.stop()
		// An acquired frame reader keeps its own transport error. Closing the
		// engine preserves the cause that reader recorded; the next reader then
		// exposes it through CloseError without changing the frame's error.
		_ = c.Conn.Close()
		_, reader, err := c.Conn.NextReader()
		if reader != nil {
			_ = reader.Close()
		}
		failed = err
	}
	if errors.As(failed, &closed) {
		switch closed.Reason {
		case engineclient.ReasonTransportClose, engineclient.ReasonPingTimeout, engineclient.ReasonTransportError:
			reason = string(closed.Reason)
		}
	}
	c.mu.Lock()
	if !c.draining {
		c.disconnectReason = reason
	}
	c.mu.Unlock()
	c.finish()
}

func (c *conn) register(nsp string, nc *namespaceConn) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !isDone(c.closing) {
		if isDone(c.engineDone()) {
			nc.cancelTypedLocked()
		}
		c.namespaces.Set(nsp, nc)
	}
	return !isDone(c.closing)
}

func (c *conn) claim(nsp string, expected ...*namespaceConn) (*namespaceConn, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	nc, ok := c.namespaces.Get(nsp)
	if len(expected) > 0 && nc != expected[0] {
		return nil, false
	}
	c.namespaces.Delete(nsp)
	if ok {
		nc.cancelTypedLocked()
	}
	return nc, ok
}

// connected ends the connect; a failure reports err first, so Emits from OnError queue, then closes.
func (c *conn) connected(err error) bool {
	var onError func(Conn, error)
	if root := c.namespace(rootNamespace); root != nil {
		onError = root.ErrorHandler()
	}
	if err != nil && onError != nil {
		onError(nil, err)
	} else if err != nil && !expected(err, c.writer.failed) {
		c.unhandled(rootNamespace, err)
	}
	c.mu.Lock()
	failed := err != nil || isDone(c.closing) && !c.draining
	if c.connecting = false; failed && !isDone(c.closing) {
		c.pending = c.startClose()
	}
	c.mu.Unlock()
	if failed {
		c.reportOverflow(nil)
		c.finish()
	}
	return !failed
}

func (f frameIO) Close() error { return f.mark(f.c.Close()) }
