package socketio

import (
	"errors"
	"reflect"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/parser"
)

func (c *connection) Close() error {
	c.mu.Lock()
	if isDone(c.closing) {
		c.mu.Unlock()
		return nil
	}
	ncs := c.startClose()
	c.disconnectReason = "server namespace disconnect"
	c.state = connectionDraining
	c.draining, c.drainTimer = true, time.AfterFunc(c.drainTimeout, c.stop)
	c.mu.Unlock()
	c.disconnectGraceful(ncs)
	c.mu.Lock()
	c.appSealed = true
	c.sealIfReady()
	c.mu.Unlock()
	return nil
}

// disconnectGraceful defers callbacks while admission or adapter room operations
// run. Close remains callable from either and the seal waits for their cleanup.
func (c *connection) disconnectGraceful(ncs []*namespaceConn) {
	for _, nc := range ncs {
		nc.outMu.Lock()
		done := nc.connectDone
		nc.outMu.Unlock()
		roomsDone := nc.roomOperationsDone()
		if done != nil && !isDone(done) || roomsDone != nil && !isDone(roomsDone) {
			c.mu.Lock()
			c.controlPending++
			c.mu.Unlock()
			go func() {
				if done != nil {
					<-done
				}
				if roomsDone != nil {
					<-roomsDone
				}
				c.disconnect([]*namespaceConn{nc})
				c.controlEnqueued()
			}()
		} else {
			c.disconnect([]*namespaceConn{nc})
		}
	}
}

// sealIfReady lets the writer finish once application writes are sealed and
// every disconnect claimed before Close has queued its control packets.
// The caller holds mu; no namespace can be claimed after startClose.
func (c *connection) sealIfReady() {
	if c.appSealed && c.controlPending == 0 && !isDone(c.seal) {
		close(c.seal)
		if c.state < connectionSealed {
			c.state = connectionSealed
		}
	}
}

func (c *connection) controlEnqueued() {
	c.mu.Lock()
	c.controlPending--
	c.sealIfReady()
	c.mu.Unlock()
}

// startClose takes the connected namespaces; c.mu is held.
func (c *connection) startClose() (ncs []*namespaceConn) {
	c.state = connectionClosing
	close(c.closing)
	c.namespaces.Range(func(_ string, nc *namespaceConn) { ncs = append(ncs, nc) })
	for _, nc := range ncs {
		nc.endTyped()
		c.namespaces.Delete(fmtNS(nc.namespace))
	}
	c.disconnects.Add(len(ncs))
	c.disconnecting += len(ncs)
	// A graceful close may owe CONNECT and DISCONNECT for each namespace
	// it just claimed; no more namespaces can register once closing starts.
	c.closeControls = 2 * c.disconnecting
	return ncs
}

// stop starts a library close, or ends a drain.
func (c *connection) stop() { c.mu.Lock(); defer c.mu.Unlock(); c.stopLocked() }

func (c *connection) stopLocked() {
	if !isDone(c.closing) {
		c.pending = c.startClose()
	}
	if !isDone(c.discard) {
		c.state = connectionDiscarded
		close(c.discard)
		if c.drainTimer != nil {
			c.drainTimer.Stop()
		}
		go func() { _ = engineio.Abort(c.Conn) }() // the emitter of an overflow must not block
	}
}

// finish ends the close: it discards, then runs the OnDisconnect calls a library close owes.
func (c *connection) finish() {
	c.mu.Lock()
	c.stopLocked()
	ncs := c.pending
	c.mu.Unlock()
	c.disconnect(ncs)
	c.mu.Lock()
	c.state = connectionClosed
	c.mu.Unlock()
	close(c.done)
}

func (c *connection) disconnect(ncs []*namespaceConn) {
	c.mu.Lock()
	reason := c.disconnectReason
	c.mu.Unlock()
	for _, nc := range ncs {
		nc.outMu.Lock()
		rejected := nc.connectErr != nil
		nc.outMu.Unlock()
		if c.draining && rejected {
			nc.reject()
			c.mu.Lock()
			c.disconnecting--
			c.mu.Unlock()
			c.disconnects.Done()
			continue
		}
		if c.draining {
			nc.acceptPending()
			c.write(parser.Header{Type: parser.Disconnect, Namespace: fmtNS(nc.namespace)})
		}
		nc.outMu.Lock()
		nc.disconnectEmits = true
		nc.outMu.Unlock()
		c.disconnectNamespace(nc, reason)
		nc.outMu.Lock()
		nc.disconnectEmits = false
		nc.outMu.Unlock()
		c.log.Debug("socketio: disconnect", nspAttr(nc.namespace), "reason", "connection close")
	}
}

// disconnectNamespace exposes rooms to disconnecting, then removes them before
// OnDisconnect, matching the reference's two lifecycle notifications.
func (c *connection) disconnectNamespace(nc *namespaceConn, reason string) {
	defer func() {
		c.mu.Lock()
		c.disconnecting--
		c.mu.Unlock()
		c.disconnects.Done()
	}()
	nh := c.namespace(fmtNS(nc.namespace))
	if nh != nil {
		if _, err := nh.DispatchEvent(nc, "disconnecting", reflect.ValueOf(reason)); err != nil {
			if onError := nh.ErrorHandler(); onError != nil {
				onError(nc, err)
			} else {
				c.unhandled(nc.namespace, err)
			}
		}
	}
	if nh != nil {
		nh.RemoveSocket(nc)
	}
	nc.retireRooms()
	if nh != nil {
		nh.NotifyDisconnect(nc, reason)
	}
	c.finishRetirement(nc)

}

// finishRead obtains the final engine cause on the read goroutine, after Decode
// returned and before finish invokes any OnDisconnect handlers.
func (c *connection) finishRead() {
	reason := "forced close"
	var closed *engineio.CloseError
	failed := c.reader.failure()
	if failed != nil && !errors.As(failed, &closed) {
		// Dispatch has ended and its error was already reported. Start the
		// close before the engine shuts down, so the writer cannot report that
		// shutdown as a second failure while the reader obtains its cause.
		c.stop()
		// An acquired frame reader keeps its own transport error. Closing the
		// engine preserves the cause that reader recorded; the next reader then
		// exposes it through CloseError without changing the frame's error.
		_ = engineio.Abort(c.Conn)
		_, reader, err := c.Conn.NextReader()
		if reader != nil {
			_ = reader.Close()
		}
		failed = err
	}
	if errors.As(failed, &closed) {
		switch closed.Reason {
		case engineio.ReasonTransportClose, engineio.ReasonPingTimeout, engineio.ReasonTransportError:
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

func (c *connection) register(nsp string, nc *namespaceConn) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if isDone(c.closing) {
		return false
	}
	if _, exists := c.namespaces.Get(nsp); exists {
		return false
	}
	if _, retiring := c.retiring[nsp]; retiring {
		return false
	}
	if isDone(c.engineDone) {
		nc.endTyped()
	}
	c.namespaces.Set(nsp, nc)
	return true
}

func (c *connection) claim(nsp string, expected ...*namespaceConn) (*namespaceConn, bool) {
	return c.claimNamespace(nsp, false, false, expected...)
}

func (c *connection) claimDisconnect(nsp string, expected ...*namespaceConn) (*namespaceConn, bool) {
	return c.claimNamespace(nsp, true, false, expected...)
}

func (c *connection) claimControlDisconnect(nsp string, expected *namespaceConn) (*namespaceConn, bool) {
	return c.claimNamespace(nsp, true, true, expected)
}

func (c *connection) claimNamespace(nsp string, disconnect, control bool, expected ...*namespaceConn) (*namespaceConn, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	nc, ok := c.namespaces.Get(nsp)
	if len(expected) > 0 && nc != expected[0] {
		return nil, false
	}
	c.namespaces.Delete(nsp)
	if ok {
		nc.endTyped()
	}
	if ok && disconnect {
		c.retiring[nsp] = nil
		c.disconnects.Add(1)
		c.disconnecting++
	}
	if ok && control {
		c.controlPending++
	}
	return nc, ok
}

func (f frameIO) Close() error { return f.mark(f.c.Close()) }

// finishRetirement schedules replacement admission on the reader after the old
// socket has left its rooms. The retiring entry remains until that task is taken,
// so another CONNECT cannot admit a replacement on a different path meanwhile.
func (c *connection) finishRetirement(nc *namespaceConn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	nsp := fmtNS(nc.namespace)
	if c.retiring[nsp] == nil || isDone(c.closing) {
		delete(c.retiring, nsp)
		return
	}
	c.reconnectReady = append(c.reconnectReady, nsp)
	select {
	case c.readReady <- struct{}{}:
	default:
	}
}

func (c *connection) runReconnects() error {
	for {
		c.mu.Lock()
		if len(c.reconnectReady) == 0 {
			c.mu.Unlock()
			return nil
		}
		nsp := c.reconnectReady[0]
		c.reconnectReady[0] = ""
		c.reconnectReady = c.reconnectReady[1:]
		header := c.retiring[nsp]
		delete(c.retiring, nsp)
		c.mu.Unlock()
		if header != nil {
			if err := connectNamespace(c, *header); err != nil {
				return err
			}
		}
	}
}
