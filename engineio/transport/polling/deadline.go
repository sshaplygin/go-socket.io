package polling

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"
)

// A polling request's handler goroutine does its network I/O: it streams a
// POST body to the session's reader through a pipe and writes a GET response
// after the session writer has filled it, so a stalled client blocks only its
// handler.
// Where the ResponseWriter supports deadlines, that I/O follows the session
// deadline. Close expires a body read, which can only be incomplete; a response
// write keeps its deadline, so packets accepted before the close (CLOSE of an
// orderly close among them) are still delivered.

// expiredDeadline is in the past; applying it interrupts blocked request I/O.
var expiredDeadline = time.Unix(1, 0)

// requestError is a failed request body read or response write. It is not a
// polling.Error, so the session does not retry it as temporary, and it reports
// Timeout for an expired deadline found through errors.As.
type requestError struct {
	op  string
	err error
}

func (e *requestError) Error() string { return e.op + ": " + e.err.Error() }
func (e *requestError) Unwrap() error { return e.err }

func (e *requestError) Timeout() bool {
	var timeout interface{ Timeout() bool }
	return errors.As(e.err, &timeout) && timeout.Timeout()
}

// requestIO is the body read (POST) or response write (GET) of one request
// whose deadline follows the session. serverConn.ioMu guards it.
type requestIO struct {
	rc      *http.ResponseController
	write   bool
	limit   time.Time // the http.Server's ReadTimeout or WriteTimeout bound, if any
	expired bool      // an expired deadline was applied
}

// apply sets the earlier of t and the server's own bound.
func (r *requestIO) apply(t time.Time) error {
	if !r.limit.IsZero() && (t.IsZero() || r.limit.Before(t)) {
		t = r.limit
	}
	if r.write {
		return r.rc.SetWriteDeadline(t)
	}
	return r.rc.SetReadDeadline(t)
}

// follow and expire skip expired I/O, so an expiry is never extended.
func (r *requestIO) follow(t time.Time) {
	if !r.expired {
		_ = r.apply(t)
	}
}

func (r *requestIO) expire() {
	if !r.expired {
		r.expired = true
		_ = r.apply(expiredDeadline)
	}
}

// requestSet is the tracked body reads or response writes of one connection.
// Overlapping requests are refused, so it usually holds one.
type requestSet map[*requestIO]struct{}

func (s requestSet) follow(t time.Time) {
	for r := range s {
		r.follow(t)
	}
}

func (s requestSet) expire() {
	for r := range s {
		r.expire()
	}
}

// track makes the body read or response write of r follow the session
// deadline until untrack. It returns nil, leaving that I/O to the http.Server,
// when w does not support the deadline (http.ErrNotSupported). start is when
// the handler started.
func (c *serverConn) track(w http.ResponseWriter, r *http.Request, start time.Time, write bool) *requestIO {
	c.ioMu.Lock()
	defer c.ioMu.Unlock()
	set, deadline := &c.reading, c.payload.readDeadline.Load().(time.Time)
	if write {
		set, deadline = &c.writing, c.payload.writeDeadline.Load().(time.Time)
	}
	tracked := &requestIO{rc: http.NewResponseController(w), write: write}
	if srv, ok := r.Context().Value(http.ServerContextKey).(*http.Server); ok {
		timeout := srv.ReadTimeout
		if write {
			timeout = srv.WriteTimeout
		}
		if timeout > 0 {
			tracked.limit = start.Add(timeout)
		}
	}
	if tracked.apply(deadline) != nil {
		return nil
	}
	if *set == nil {
		*set = requestSet{}
	}
	(*set)[tracked] = struct{}{}
	if c.ioClosed && !write {
		tracked.expire()
	}
	return tracked
}

// untrack ends tracked and reports whether it expired. After a complete body
// (read) it clears a deadline the session set while net/http began its own read.
func (c *serverConn) untrack(tracked *requestIO, read bool) bool {
	if tracked == nil {
		return false
	}
	c.ioMu.Lock()
	defer c.ioMu.Unlock()
	if tracked.write {
		delete(c.writing, tracked)
	} else {
		delete(c.reading, tracked)
	}
	if read && !tracked.expired {
		_ = tracked.rc.SetReadDeadline(time.Time{})
	}
	return tracked.expired
}

// SetReadDeadline bounds NextReader and the tracked POST body reads.
func (c *serverConn) SetReadDeadline(t time.Time) error {
	c.ioMu.Lock()
	defer c.ioMu.Unlock()
	_ = c.payload.SetReadDeadline(t)
	c.reading.follow(t)
	return nil
}

// SetWriteDeadline bounds NextWriter and the tracked GET response writes.
func (c *serverConn) SetWriteDeadline(t time.Time) error {
	c.ioMu.Lock()
	defer c.ioMu.Unlock()
	_ = c.payload.SetWriteDeadline(t)
	c.writing.follow(t)
	return nil
}

// Close closes the connection and expires the tracked POST body reads.
func (c *serverConn) Close() error {
	c.ioMu.Lock()
	c.ioClosed = true
	c.reading.expire()
	c.ioMu.Unlock()
	return c.payload.Close()
}

// feedBody streams a POST body to the session through a pipe. The handler's
// goroutine copies it from the network; the feed runs on its own goroutine and
// closes the pipe when it returns, on a deadline, Close or the end of the
// payload, so the session's reader never waits for the client. A body the
// payload cannot take (overlapping, paused or closed) is refused unread. The
// body's Close drains what the handler did not read, so that net/http can
// reuse the connection, under the same deadline.
func (c *serverConn) feedBody(w http.ResponseWriter, r *http.Request, start time.Time, body io.ReadCloser, supportBinary bool) error {
	tracked := c.track(w, r, start, false)
	var err error
	defer func() {
		_ = body.Close()
		if c.untrack(tracked, err == nil) && r.ProtoMajor == 1 {
			// The expired deadline may have failed net/http's read after the body,
			// which cancels the connection's context; do not reuse the connection.
			w.Header().Set("Connection", "close")
		}
	}()
	if err = c.payload.acquireFeed(); err != nil {
		return err
	}
	in, out := io.Pipe()
	fed := make(chan error, 1)
	go func() {
		err := c.payload.feed(in, supportBinary)
		_ = in.Close()
		fed <- err
	}()
	buf := copyBuffers.Get().(*[]byte)
	_, err = io.CopyBuffer(out, body, *buf)
	copyBuffers.Put(buf)
	if err != nil {
		_ = out.CloseWithError(&requestError{"request body", err})
	} else {
		_ = out.Close()
	}
	return <-fed
}

var copyBuffers = sync.Pool{New: func() any { b := make([]byte, 4096); return &b }}

// writeResponse writes a filled GET response on the handler's goroutine. A
// failure closes the connection, as a failed packet write did.
func (c *serverConn) writeResponse(w http.ResponseWriter, r *http.Request, start time.Time, p []byte) error {
	tracked := c.track(w, r, start, true)
	_, err := w.Write(p)
	c.untrack(tracked, false)
	if err != nil {
		_ = c.payload.Store("write", &requestError{"response", err})
		_ = c.Close()
	}
	return err
}

// responseBuffer collects a GET response; headers go to the ResponseWriter.
type responseBuffer struct {
	http.ResponseWriter
	buf bytes.Buffer
}

func (b *responseBuffer) Write(p []byte) (int, error) { return b.buf.Write(p) }
