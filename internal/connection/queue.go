package connection

import (
	"reflect"

	"github.com/sshaplygin/go-socket.io/parser"
)

func (c *Connection) write(header parser.Header, args ...reflect.Value) {
	c.writeFlags(header, false, false, args...)
}

func (c *Connection) writeFlags(header parser.Header, reserved, noCompress bool, args ...reflect.Value) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queueFlagsLocked(header, reserved, noCompress, args...)
}

// queueFlagsLocked admits a packet with the queue budget and close state locked.
func (c *Connection) queueFlagsLocked(header parser.Header, reserved, noCompress bool, args ...reflect.Value) {
	data := make([]interface{}, len(args))

	for i := range data {
		if args[i].IsValid() {
			data[i] = args[i].Interface()
		}
	}

	pkg := outboundPayload{Payload: parser.Payload{Header: header, Data: data}, noCompress: noCompress}

	if reserved {
		c.pendingWrites--
	}
	control := header.Type == parser.Connect || header.Type == parser.Disconnect
	switch {
	case isDone(c.closing) && (!c.draining || isDone(c.seal) || isDone(c.discard) || c.appSealed && !control && !reserved):
	case c.boundary || c.draining:
		// Server lifecycle packets share ordering with application packets but
		// do not consume the application budget reserved by pending Emits.
		// Reserve a finite pair of lifecycle slots per application slot. This
		// admits CONNECT followed by DISCONNECT even with a one-slot budget.
		controlSpace := control && c.controlQueued < 2*cap(c.writeChan) && (header.Type == parser.Disconnect || len(c.writeChan)+c.drainQueued < cap(c.writeChan))
		if control && c.draining {
			controlSpace = c.closeControls > 0
		}
		if controlSpace || reserved || !control && len(c.writeChan)+c.drainQueued+c.pendingWrites < cap(c.writeChan) {
			c.drainQueue = append(c.drainQueue, drainPacket{pkg, control})
			if control {
				c.controlQueued++
				if c.draining {
					c.closeControls--
				}
			} else {
				c.drainQueued++
			}
			c.notifyWrite()
		} else if !c.draining {
			c.overflowWrite(header)
		}
	case len(c.writeChan) < cap(c.writeChan) && (reserved || len(c.writeChan)+c.pendingWrites < cap(c.writeChan)):
		c.writeChan <- pkg
		c.notifyWrite()
	case !isDone(c.closing):
		c.overflowWrite(header)
	}

}

// overflowWrite starts a library close for a full outbound queue; mu is held.
func (c *Connection) overflowWrite(header parser.Header) {
	if isDone(c.closing) {
		return
	}
	if c.overflow = c.errConn(header.Namespace); c.overflow == nil {
		c.overflow = &namespaceConn{Connection: c, namespace: header.Namespace}
	}
	if c.pending = c.startClose(); !c.connecting {
		c.stopLocked()
	}

}

func isDone(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

type drainPacket struct {
	payload outboundPayload
	control bool
}

// notifyWrite is called under mu; the signal is only a hint to recheck the queues.
func (c *Connection) notifyWrite() {
	select {
	case c.writeReady <- struct{}{}:
	default:
	}
}

func (c *Connection) nextQueued() (outboundPayload, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.writeChan) > 0 {
		return <-c.writeChan, true
	}
	if len(c.drainQueue) > 0 {
		packet := c.drainQueue[0]
		c.drainQueue[0] = drainPacket{}
		c.drainQueue = c.drainQueue[1:]
		if packet.control {
			c.controlQueued--
		} else {
			c.drainQueued--
		}
		return packet.payload, true
	}
	return outboundPayload{}, false
}

// outboundPayload keeps transport flags out of the shared parser codec.
type outboundPayload struct {
	parser.Payload
	noCompress bool
}
