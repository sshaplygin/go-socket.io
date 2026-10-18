package connection

import (
	"reflect"

	"github.com/sshaplygin/go-socket.io/parser"
)

func (c *conn) write(header parser.Header, args ...reflect.Value) {
	c.writeFlags(header, false, args...)
}

func (c *conn) writeFlags(header parser.Header, noCompress bool, args ...reflect.Value) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queueFlagsLocked(header, noCompress, args...)
}

// queueFlagsLocked admits a packet with the queue budget and close state locked.
func (c *conn) queueFlagsLocked(header parser.Header, noCompress bool, args ...reflect.Value) {
	data := make([]interface{}, len(args))
	for i := range data {
		if args[i].IsValid() {
			data[i] = args[i].Interface()
		}
	}
	if isDone(c.closing) && (!c.draining || c.appSealed || isDone(c.discard)) {
		return
	}
	if len(c.writeChan) < cap(c.writeChan) {
		c.enqueued++
		c.writeChan <- queuedPayload{payload: outboundPayload{Payload: parser.Payload{Header: header, Data: data}, noCompress: noCompress}, sequence: c.enqueued}
		c.notifyWrite()
	} else if !isDone(c.closing) {
		c.overflowWrite(header)
	}
}

// overflowWrite starts a library close for a full outbound queue; mu is held.
func (c *conn) overflowWrite(header parser.Header) {
	if isDone(c.closing) {
		return
	}
	if c.overflow = c.errConn(header.Namespace); c.overflow == nil {
		c.overflow = &namespaceConn{conn: c, namespace: header.Namespace}
	}
	if c.pending = c.startClose(); !c.connecting {
		c.stopLocked()
	}

}

// notifyWrite is called under mu; the signal is only a hint to recheck the queues.
func (c *conn) notifyWrite() {
	select {
	case c.writeReady <- struct{}{}:
	default:
	}
}

func (c *conn) nextQueued() (outboundPayload, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.controls) > 0 && c.controls[0].after <= c.started {
		control := c.controls[0]
		c.controls = c.controls[1:]
		return outboundPayload{Payload: control.payload}, true
	}
	if len(c.writeChan) > 0 {
		queued := <-c.writeChan
		c.started = queued.sequence
		return queued.payload, true
	}
	return outboundPayload{}, false
}

type queuedPayload struct {
	payload  outboundPayload
	sequence uint64
}

type controlPayload struct {
	payload parser.Payload
	after   uint64
}

// Lifecycle packets retain their position among application writes without
// consuming an application slot. At most one CONNECT/DISCONNECT is owed per socket.
func (c *conn) writeControl(header parser.Header) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if isDone(c.discard) || c.appSealed {
		return
	}
	c.controls = append(c.controls, controlPayload{payload: parser.Payload{Header: header}, after: c.enqueued})
	c.notifyWrite()
}

func (c *conn) queueReadTask(task func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if isDone(c.closing) {
		return
	}
	c.readTasks = append(c.readTasks, task)
	select {
	case c.readReady <- struct{}{}:
	default:
	}
}

func (c *conn) runReadTasks() {
	c.mu.Lock()
	tasks := c.readTasks
	c.readTasks = nil
	c.mu.Unlock()
	for _, task := range tasks {
		task()
	}
}

// outboundPayload keeps transport flags out of the shared parser codec.
type outboundPayload struct {
	parser.Payload
	noCompress bool
}
