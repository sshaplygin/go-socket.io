package socketio

import (
	"reflect"

	"github.com/sshaplygin/go-socket.io/parser"
)

func (c *connection) write(header parser.Header, args ...reflect.Value) {
	c.writeFlags(header, false, false, args...)
}

func (c *connection) writeFlags(header parser.Header, reserved, noCompress bool, args ...reflect.Value) {
	pkg := preparePayload(header, noCompress, args...)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queuePayloadLocked(pkg, reserved)
}

// writeAck admits a reply only while its originating namespace is current.
// Encoding runs before the lock because application marshalers may reenter.
func (nc *namespaceConn) writeAck(header parser.Header, args ...reflect.Value) {
	header.Type = parser.Ack
	pkg := preparePayload(header, false, args...)
	c := nc.connection
	c.mu.Lock()
	defer c.mu.Unlock()
	current, ok := c.namespaces.Get(fmtNS(nc.namespace))
	if !ok || current != nc || isDone(c.closing) {
		return
	}
	c.queuePayloadLocked(pkg, false)
}

func preparePayload(header parser.Header, noCompress bool, args ...reflect.Value) outboundPayload {
	data := make([]interface{}, len(args))
	for i := range data {
		if args[i].IsValid() {
			data[i] = args[i].Interface()
		}
	}
	var packet *parser.PreparedPacket
	var err error
	switch header.Type {
	case parser.Connect, parser.Disconnect:
		packet, err = parser.Prepare(header)
	case parser.Error:
		packet, err = parser.Prepare(header, data...)
	default:
		packet, err = parser.Prepare(header, data)
	}
	return outboundPayload{Header: header, prepared: packet, err: err, noCompress: noCompress}
}

// queuePayloadLocked admits an immutable packet under the queue budget lock.
func (c *connection) queuePayloadLocked(pkg outboundPayload, reserved bool) {
	header := pkg.Header

	if reserved {
		c.pendingWrites--
	}
	control := header.Type == parser.Connect || header.Type == parser.Disconnect
	switch {
	case isDone(c.closing) && (!c.draining || isDone(c.seal) || isDone(c.discard) || c.appSealed && !control && !reserved && !pkg.lifecycle):
	default:
		// Server lifecycle packets share ordering with application packets but
		// do not consume the application budget reserved by pending Emits.
		// Reserve a finite pair of lifecycle slots per application slot. This
		// admits CONNECT followed by DISCONNECT even with a one-slot budget.
		controlSpace := control && c.controlQueued < 2*c.writeBufferSize && (header.Type == parser.Disconnect || c.drainQueued < c.writeBufferSize)
		if control && c.draining {
			controlSpace = c.closeControls > 0
		}
		if controlSpace || reserved || !control && c.drainQueued+c.pendingWrites < c.writeBufferSize {
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
	}

}

// overflowWrite starts a library close for a full outbound queue; mu is held.
func (c *connection) overflowWrite(header parser.Header) {
	if isDone(c.closing) {
		return
	}
	if c.overflow = c.errConn(header.Namespace); c.overflow == nil {
		c.overflow = &namespaceConn{connection: c, namespace: header.Namespace}
	}
	if c.pending = c.startClose(); !c.connecting {
		c.startError()
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
func (c *connection) notifyWrite() {
	select {
	case c.writeReady <- struct{}{}:
	default:
	}
}

func (c *connection) nextQueued() (outboundPayload, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
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
	Header     parser.Header
	prepared   *parser.PreparedPacket
	err        error
	noCompress bool
	lifecycle  bool
}
