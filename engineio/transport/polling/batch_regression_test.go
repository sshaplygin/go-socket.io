package polling

import (
	"bytes"
	"compress/gzip"
	"context"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
	"github.com/stretchr/testify/require"
	"io"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Covers G24
func TestPollingBatchesOrderedPacketsInOneResponse(t *testing.T) {
	for _, encoding := range []string{"", "gzip"} {
		t.Run(encoding, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://localhost/?b64=1", nil)
			req.Header.Set("Accept-Encoding", encoding)
			c := newServerConn(&Transport{Compression: true, CompressionThreshold: 1}, req)
			defer c.Close()
			require.NoError(t, c.SetWriteDeadline(time.Now().Add(time.Second)))
			done := make(chan error, 1)
			go func() {
				for _, body := range []string{"first", "second", "third"} {
					w, e := c.NextWriter(eioparser.String, eioparser.MESSAGE)
					if e != nil {
						done <- e
						return
					}
					_, e = io.WriteString(w, body)
					if e == nil {
						e = w.Close()
					}
					if e != nil {
						done <- e
						return
					}
				}
				done <- nil
			}()
			response := httptest.NewRecorder()
			c.ServeHTTP(response, req)
			body := response.Body.Bytes()
			if encoding != "" {
				r, e := gzip.NewReader(bytes.NewReader(body))
				require.NoError(t, e)
				body, e = io.ReadAll(r)
				require.NoError(t, e)
				require.NoError(t, r.Close())
			}
			require.Equal(t, "6:4first7:4second6:4third", string(body))
			require.NoError(t, <-done)
		})
	}
}

// Covers G24
func TestPollingBatchContainsBinaryAndCloseInOrder(t *testing.T) {
	for _, query := range []string{"?b64=1", ""} {
		t.Run(query, func(t *testing.T) {
			req := httptest.NewRequest("GET", "http://localhost/"+query, nil)
			c := newServerConn(Default, req)
			defer c.Close()
			require.NoError(t, c.SetWriteDeadline(time.Now().Add(time.Second)))
			done := make(chan error, 1)
			go func() {
				for _, ft := range []eioparser.FrameType{eioparser.String, eioparser.Binary} {
					w, e := c.NextWriter(ft, eioparser.MESSAGE)
					if e != nil {
						done <- e
						return
					}
					_, e = w.Write([]byte("a"))
					if e == nil {
						e = w.Close()
					}
					if e != nil {
						done <- e
						return
					}
				}
				done <- c.CloseGracefully()
			}()
			response := httptest.NewRecorder()
			c.ServeHTTP(response, req)
			require.NoError(t, <-done)
			if query != "" {
				require.Equal(t, "2:4a6:b4YQ==1:1", response.Body.String())
			} else {
				require.Equal(t, []byte{0, 2, 255, '4', 'a', 1, 2, 255, 4, 'a', 0, 1, 255, '1'}, response.Body.Bytes())
			}
		})
	}
}

// Covers G24
func TestPollingBatchHasFinitePacketBudget(t *testing.T) {
	req := httptest.NewRequest("GET", "http://localhost/?b64=1", nil)
	c := newServerConn(Default, req)
	defer c.Close()
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			w, e := c.NextWriter(eioparser.String, eioparser.MESSAGE)
			if e != nil {
				return
			}
			if w.Close() != nil {
				return
			}
		}
	}()
	done := make(chan struct{})
	response := httptest.NewRecorder()
	go func() { defer close(done); c.ServeHTTP(response, req) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("sustained writes retained the GET")
	}
	require.NotEmpty(t, response.Body.String())
	require.LessOrEqual(t, len(response.Body.String()), 64*3)
	require.NoError(t, c.Close())
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("writer did not stop")
	}
}

type secondPacketGate struct {
	writes atomic.Int32
	gate   gatedWriter
}

func (w *secondPacketGate) Write(p []byte) (int, error) {
	if w.writes.Add(1) >= 3 {
		return w.gate.Write(p)
	}
	return len(p), nil
}

// Covers G11
// Covers G24
func TestCanceledBatchKeepsActiveWriterLease(t *testing.T) {
	p := newPayload(false)
	p.batch = true
	defer p.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &secondPacketGate{gate: gatedWriter{started: make(chan struct{}), release: make(chan struct{})}}
	var release sync.Once
	defer release.Do(func() { close(w.gate.release) })
	written := make(chan struct{})
	go func() {
		defer close(written)
		for i := 0; i < 2; i++ {
			out, e := p.NextWriter(eioparser.String, eioparser.MESSAGE)
			if e != nil {
				return
			}
			_, _ = out.Write([]byte("a"))
			if out.Close() != nil {
				return
			}
		}
	}()
	flushed := make(chan error, 1)
	go func() { err := p.flushOut(ctx, w); w.gate.returned.Store(true); flushed <- err }()
	select {
	case <-w.gate.started:
	case <-time.After(time.Second):
		t.Fatal("batch did not acquire second packet")
	}
	cancel()
	select {
	case <-p.close:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not close payload")
	}
	select {
	case <-flushed:
		t.Fatal("flush returned while writer was active")
	default:
	}
	release.Do(func() { close(w.gate.release) })
	select {
	case e := <-flushed:
		require.Error(t, e)
	case <-time.After(time.Second):
		t.Fatal("flush retained writer")
	}
	<-written
	require.Zero(t, w.gate.late.Load())
}
