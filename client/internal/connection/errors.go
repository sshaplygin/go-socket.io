package connection

import (
	"cmp"
	"errors"
	"log/slog"

	"github.com/sshaplygin/go-socket.io/client/internal/namespace"
)

// reportOverflow reports an overflow with conn, nil on a failed connect as in v1.4.
func (c *conn) reportOverflow(conn namespace.Conn) {
	if c.overflow == nil {
		return
	}
	if nh := c.namespace(fmtNS(c.overflow.namespace)); nh != nil {
		if f := nh.ErrorHandler(); f != nil {
			f(conn, ErrWriteBufferFull)
			return
		}
	}
	c.unhandled(c.overflow.namespace, ErrWriteBufferFull)
}

// unhandled logs err, which no OnError receives and which is not expected closure.
func (c *conn) unhandled(nsp string, err error) {
	c.log.Warn("socketio: unhandled error", nspAttr(nsp), "err", err)
}

// nspAttr is the nsp attribute of namespace nsp; the root namespace is "/".
func nspAttr(nsp string) slog.Attr {
	return slog.String("nsp", cmp.Or(nsp, aliasRootNamespace))
}

// errConn returns the namespace.Conn to report an error of nsp with (new if nsp was disconnected), or nil.
func (c *conn) errConn(nsp string) (nc *namespaceConn) {
	if nh := c.namespace(nsp); nh != nil && nh.ErrorHandler() != nil {
		if nc, _ = c.namespaces.Get(nsp); nc == nil {
			nc = newNamespaceConn(c, cmp.Or(nsp, aliasRootNamespace), nh.Rooms())
		}
	}
	return nc
}

// onError reports err of the packet being read (see report); a failure of the frame
// reader that the decoder returned is expected closure.
func (c *conn) onError(namespace string, err error) {
	c.report(namespace, err, c.reader.failed)
}

// report delivers err to OnError of namespace and waits for it, unless a close started.
// Without OnError, err is logged as unhandled unless it is the frame failure failed.
func (c *conn) report(namespace string, err, failed error) {
	if isDone(c.closing) {
		return
	}
	msg := newErrorMessage(namespace, err)
	if msg.conn = c.errConn(namespace); msg.conn == nil {
		if !expected(err, failed) {
			c.unhandled(namespace, err)
		}
		return
	}
	select {
	case c.errorChan <- msg:
		<-msg.done
	case <-c.closing:
	}
}

var errUnavailableRootHandler = errors.New("root ('/') doesn't have a namespace handler")

var errFailedConnectNamespace = errors.New("failed connect to namespace without handler")

var errHandleDispatch = errors.New("handler dispatch error")

var errDecodeArgs = errors.New("decode args error")

// ErrWriteBufferFull is reported once, to OnError of the packet's namespace and unordered with
// OnDisconnect, when an Emit or a packet the library queues (an ACK or CONNECT reply) finds the
// outbound queue (WithWriteBufferSize) full before any close started; a packet being
// written does not count. Emit never blocks: that packet and every later one are dropped and the
// connection is closed without draining. An overflow in root OnConnect fails the connect and is
// reported with a nil namespace.Conn. Packets queued faster than they are written can close a healthy client:
// polling writes one engine.io frame per round trip, and a packet with k binary attachments takes k+1
// frames.
var ErrWriteBufferFull = errors.New("write buffer full")

// ErrServerOnly means a client connection cannot perform a server broadcast.
var ErrServerOnly = errors.New("socketio: server-only method on a client connection")

type errorMessage struct {
	namespace string
	conn      *namespaceConn // nil if namespace has no OnError; see conn.errConn
	done      chan struct{}  // closed once OnError returned

	err error
}

func newErrorMessage(namespace string, err error) *errorMessage {
	return &errorMessage{
		namespace: namespace,
		done:      make(chan struct{}),
		err:       err,
	}
}
