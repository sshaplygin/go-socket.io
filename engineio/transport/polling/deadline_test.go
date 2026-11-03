package polling

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type boundedResponse struct {
	*httptest.ResponseRecorder
	mu        sync.Mutex
	deadlines []time.Time
}

func (w *boundedResponse) SetReadDeadline(d time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deadlines = append(w.deadlines, d)
	return nil
}
func (w *boundedResponse) SetWriteDeadline(d time.Time) error { return w.SetReadDeadline(d) }
func (w *boundedResponse) snapshot() []time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]time.Time(nil), w.deadlines...)
}

// net/http starts its read timeout before parsing headers, and its write timeout
// before dispatching the handler. Neither absolute deadline is exposed to handlers.
func TestRequestDeadlinePreservesServerBound(t *testing.T) {
	for _, write := range []bool{false, true} {
		t.Run(map[bool]string{false: "read", true: "write"}[write], func(t *testing.T) {
			c := &serverConn{payload: newPayload(true)}
			w := &boundedResponse{ResponseRecorder: httptest.NewRecorder()}
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			r = r.WithContext(context.WithValue(r.Context(), http.ServerContextKey, &http.Server{ReadTimeout: time.Second, WriteTimeout: time.Second}))
			// Headers consumed most of the timeout before this handler started. A session
			// deadline 600ms away must not replace the server's remaining 300ms bound.
			session := time.Now().Add(600 * time.Millisecond)
			if write {
				_ = c.SetWriteDeadline(session)
			} else {
				_ = c.SetReadDeadline(session)
			}
			tracked := c.track(w, r, write)
			if tracked == nil {
				t.Fatal("request not tracked")
			}
			c.untrack(tracked, true)
			for _, d := range w.snapshot() {
				if d.IsZero() || d.After(time.Now()) {
					t.Fatalf("replaced server bound with %v", d)
				}
			}
		})
	}
}

func TestDelayedHeadersPreserveReadTimeout(t *testing.T) {
	result := make(chan error, 1)
	entered := make(chan struct{})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := &serverConn{payload: newPayload(true)}
		_ = c.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
		tracked := c.track(w, r, false)
		close(entered)
		_, err := io.ReadAll(r.Body)
		c.untrack(tracked, err == nil)
		result <- err
	}))
	server.Config.ReadTimeout = 2 * time.Second
	server.Start()
	defer server.Close()
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close test connection: %v", err)
		}
	}()
	_, err = fmt.Fprint(conn, "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 1\r\n")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Second)
	_, err = fmt.Fprint(conn, "\r\n")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("handler not entered")
	}
	// This byte arrives after the original timeout but before the incorrectly
	// extended session deadline. The body must already have failed.
	time.Sleep(1200 * time.Millisecond)
	_, _ = conn.Write([]byte("x"))
	select {
	case err := <-result:
		timeout, ok := err.(net.Error)
		if !ok || !timeout.Timeout() {
			t.Fatalf("body read = %v, want original server timeout", err)
		}
	case <-time.After(time.Second):
		t.Fatal("body did not time out")
	}
}

func TestBoundedRequestSessionDeadline(t *testing.T) {
	for _, write := range []bool{false, true} {
		t.Run(map[bool]string{false: "read", true: "write"}[write], func(t *testing.T) {
			c := &serverConn{payload: newPayload(true)}
			w := &boundedResponse{ResponseRecorder: httptest.NewRecorder()}
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			r = r.WithContext(context.WithValue(r.Context(), http.ServerContextKey, &http.Server{ReadTimeout: time.Hour, WriteTimeout: time.Hour}))
			tracked := c.track(w, r, write)
			defer c.untrack(tracked, false)
			deadline := time.Now().Add(20 * time.Millisecond)
			if write {
				_ = c.SetWriteDeadline(deadline)
			} else {
				_ = c.SetReadDeadline(deadline)
			}
			until := time.Now().Add(time.Second)
			for len(w.snapshot()) == 0 && time.Now().Before(until) {
				time.Sleep(time.Millisecond)
			}
			ds := w.snapshot()
			if len(ds) != 1 || !ds[0].Equal(expiredDeadline) {
				t.Fatalf("deadlines = %v", ds)
			}
			if write {
				_ = c.SetWriteDeadline(time.Now().Add(time.Hour))
			} else {
				_ = c.SetReadDeadline(time.Now().Add(time.Hour))
			}
			if len(w.snapshot()) != 1 {
				t.Fatal("expired I/O was extended")
			}
		})
	}
}

func TestDelayedHandlerPreservesWriteTimeout(t *testing.T) {
	result := make(chan error, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Middleware or waiting for the polling payload can consume the write budget.
		time.Sleep(time.Second)
		c := &serverConn{payload: newPayload(true)}
		_ = c.SetWriteDeadline(time.Now().Add(1500 * time.Millisecond))
		tracked := c.track(w, r, true)
		defer c.untrack(tracked, false)
		time.Sleep(1200 * time.Millisecond)
		_, err := w.Write([]byte("response"))
		if err == nil {
			err = http.NewResponseController(w).Flush()
		}
		result <- err
	}))
	server.Config.WriteTimeout = 2 * time.Second
	server.Start()
	defer server.Close()
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close test connection: %v", err)
		}
	}()
	_, err = fmt.Fprint(conn, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		timeout, ok := err.(net.Error)
		if !ok || !timeout.Timeout() {
			t.Fatalf("response write = %v, want original server timeout", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not finish")
	}
}

func TestBoundedRequestCloseAbortAndUntrack(t *testing.T) {
	for _, action := range []string{"close", "abort", "untrack", "clear"} {
		t.Run(action, func(t *testing.T) {
			c := &serverConn{payload: newPayload(true)}
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			r = r.WithContext(context.WithValue(r.Context(), http.ServerContextKey, &http.Server{ReadTimeout: time.Hour, WriteTimeout: time.Hour}))
			read := &boundedResponse{ResponseRecorder: httptest.NewRecorder()}
			write := &boundedResponse{ResponseRecorder: httptest.NewRecorder()}
			rd := c.track(read, r, false)
			wd := c.track(write, r, true)
			defer c.untrack(rd, false)
			defer c.untrack(wd, false)
			deadline := time.Now().Add(30 * time.Millisecond)
			if action == "close" || action == "abort" {
				deadline = time.Now().Add(time.Hour)
			}
			_ = c.SetReadDeadline(deadline)
			_ = c.SetWriteDeadline(deadline)
			switch action {
			case "close":
				_ = c.Close()
				if len(read.snapshot()) != 1 || len(write.snapshot()) != 0 {
					t.Fatal("Close must expire only body reads")
				}
			case "abort":
				_ = c.Abort()
				if len(read.snapshot()) != 1 || len(write.snapshot()) != 1 {
					t.Fatal("Abort must expire both directions")
				}
			case "untrack":
				c.untrack(rd, true)
				c.untrack(wd, false)
			case "clear":
				_ = c.SetReadDeadline(time.Time{})
				_ = c.SetWriteDeadline(time.Time{})
			}
			if action == "clear" || action == "untrack" {
				time.Sleep(60 * time.Millisecond)
				if len(read.snapshot()) != 0 || len(write.snapshot()) != 0 {
					t.Fatal("cancelled timer changed server deadlines")
				}
			}
		})
	}
}
