package polling

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"
)

// A polling GET writes its filled response on the handler goroutine. A POST
// with a supported request deadline lets the session decoder read the body;
// otherwise its handler streams the body through a pipe, so unsupported
// request I/O cannot block the session. Close expires a body read, which can
// only be incomplete; a response write keeps its deadline, so packets
// accepted before the close (CLOSE of an orderly close among them) are still
// delivered. Abort expires both.

// expiredDeadline is in the past; applying it interrupts blocked request I/O.
var expiredDeadline = time.Unix(1, 0)

// requestError is a failed request body read or response write. It is not a
// polling.Error, so the session does not retry it as temporary, and it reports
// Timeout for an expired deadline found through errors.As.
type requestError struct {
	op  string
	err error
}

// requestBody keeps network read errors distinct from temporary payload
// errors. ReadByte lets the decoder consume framing without allocating its
// own bufio.Reader. A read that returns a byte and an error defers that error
// until the next call, as bufio.Reader.ReadByte does.
type requestBody struct {
	io.Reader
	one     [1]byte
	pending error
}

func requestReadError(err error) error {
	if err != nil && err != io.EOF {
		return &requestError{"request body", err}
	}
	return err
}

func (b *requestBody) Read(p []byte) (int, error) {
	if b.pending != nil {
		err := b.pending
		b.pending = nil
		return 0, err
	}
	n, err := b.Reader.Read(p)
	return n, requestReadError(err)
}

func (b *requestBody) ReadByte() (byte, error) {
	if b.pending != nil {
		err := b.pending
		b.pending = nil
		return 0, err
	}
	for i := 0; i < 100; i++ {
		n, err := b.Reader.Read(b.one[:])
		if n == 1 {
			b.pending = requestReadError(err)
			return b.one[0], nil
		}
		if err != nil {
			return 0, requestReadError(err)
		}
	}
	return 0, &requestError{"request body", io.ErrNoProgress}
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
	w          http.ResponseWriter
	write      bool
	bounded    bool // net/http owns a timeout whose absolute deadline is unknown
	owner      *serverConn
	timer      *time.Timer
	generation uint64
	set        bool // a deadline was applied
	expired    bool // an expired deadline was applied
}

// cancelTimer invalidates callbacks that may already be waiting for ioMu.
func (r *requestIO) cancelTimer() {
	r.generation++
	if r.timer != nil {
		r.timer.Stop()
		r.timer = nil
	}
}

// An http.Server timeout starts before the handler and its absolute deadline
// cannot be recovered here. Never replace it with a future deadline or clear it.
// Instead, interrupt I/O only when the session deadline actually expires.
func (r *requestIO) apply(t time.Time) error {
	r.cancelTimer()
	if r.bounded && (t.IsZero() || t.After(time.Now())) {
		if !t.IsZero() {
			generation := r.generation
			owner := r.owner
			r.timer = time.AfterFunc(time.Until(t), func() { r.expireGeneration(owner, generation) })
		}
		return nil
	}
	r.set = true
	return setRequestDeadline(r.w, r.write, t)
}

// expireGeneration also checks callbacks already waiting on ioMu after their
// request has been untracked and its slot has been reused.
func (r *requestIO) expireGeneration(owner *serverConn, generation uint64) {
	owner.ioMu.Lock()
	defer owner.ioMu.Unlock()
	if r.generation == generation {
		r.expire()
	}
}

// setRequestDeadline follows the same ResponseWriter/Unwrap path as
// http.ResponseController. Keeping the writer on requestIO avoids allocating a
// ResponseController for every polling request while preserving wrapper support.
func setRequestDeadline(w http.ResponseWriter, write bool, deadline time.Time) error {
	for {
		if write {
			if setter, ok := w.(interface{ SetWriteDeadline(time.Time) error }); ok {
				return setter.SetWriteDeadline(deadline)
			}
		} else if setter, ok := w.(interface{ SetReadDeadline(time.Time) error }); ok {
			return setter.SetReadDeadline(deadline)
		}
		unwrapper, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return http.ErrNotSupported
		}
		w = unwrapper.Unwrap()
	}
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
// when applying a deadline fails (including http.ErrNotSupported). With a
// server timeout, support is discovered only when an expiry is applied.
func (c *serverConn) track(w http.ResponseWriter, r *http.Request, write bool) *requestIO {
	c.ioMu.Lock()
	defer c.ioMu.Unlock()
	set, deadline, closed := &c.reading, c.payload.readDeadline.Load().(time.Time), c.ioClosed
	slot, busy := &c.readIO, &c.readBusy
	if write {
		set, deadline, closed = &c.writing, c.payload.writeDeadline.Load().(time.Time), c.ioAborted
		slot, busy = &c.writeIO, &c.writeBusy
	}
	var tracked *requestIO
	if !*busy {
		*busy = true
		tracked = slot
		generation := tracked.generation
		*tracked = requestIO{w: w, write: write, expired: closed, owner: c, generation: generation}
	} else {
		// An overlapping handler still needs its own deadline while its body
		// is closed and drained. It cannot take the active handler's slot.
		tracked = &requestIO{w: w, write: write, expired: closed, owner: c}
	}
	if closed {
		deadline = expiredDeadline
	}
	if srv, ok := r.Context().Value(http.ServerContextKey).(*http.Server); ok {
		timeout := srv.ReadTimeout
		if write {
			timeout = srv.WriteTimeout
		}
		if timeout > 0 {
			tracked.bounded = true
		}
	}
	if tracked.apply(deadline) != nil {
		if tracked == slot {
			*busy = false
			tracked.w = nil
		}
		return nil
	}
	if *set == nil {
		*set = requestSet{}
	}
	(*set)[tracked] = struct{}{}
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
	tracked.cancelTimer()
	if read && tracked.set && !tracked.expired && !tracked.bounded {
		_ = setRequestDeadline(tracked.w, false, time.Time{})
	}
	expired := tracked.expired
	if tracked == &c.readIO {
		c.readBusy = false
		tracked.w = nil
	} else if tracked == &c.writeIO {
		c.writeBusy = false
		tracked.w = nil
	}
	return expired
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

// Close closes the connection and expires the tracked POST body reads. A
// response write keeps its deadline: an orderly close delivers CLOSE through it.
func (c *serverConn) Close() error {
	c.ioMu.Lock()
	c.ioClosed = true
	c.reading.expire()
	c.ioMu.Unlock()
	return c.payload.Close()
}

// Abort closes the connection and expires the tracked response writes too. The
// session uses it for every close that is not orderly.
func (c *serverConn) Abort() error {
	c.ioMu.Lock()
	c.ioAborted = true
	c.writing.expire()
	c.ioMu.Unlock()
	return c.Close()
}

// feedBody lets the session decoder read directly when a tracked deadline can
// interrupt it. Otherwise the handler copies through a pipe and the feed runs
// on its own goroutine, so the session can end while unsupported or
// server-bounded request I/O remains blocked. A body the payload cannot take
// (overlapping, paused or closed) is refused unread. The body's Close drains
// what the handler did not read, so that net/http can reuse the connection,
// under the same deadline.
func (c *serverConn) feedBody(w http.ResponseWriter, r *http.Request, body io.ReadCloser, supportBinary bool) error {
	tracked := c.track(w, r, false)
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
	if tracked != nil && !tracked.bounded {
		// A supported request deadline interrupts a body read even when the
		// decoder owns it. That lets this handler feed the body directly, with
		// no pipe or per-POST goroutine. For unsupported or server-bounded
		// requests, the pipe still keeps network I/O off the session reader.
		err = c.payload.feed(&requestBody{Reader: body}, supportBinary)
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
func (c *serverConn) writeResponse(w http.ResponseWriter, r *http.Request, p []byte) error {
	tracked := c.track(w, r, true)
	defer c.untrack(tracked, false)
	_, err := w.Write(p)
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
