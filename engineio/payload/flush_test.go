package payload

import (
	"bytes"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
)

// writeAsync writes one MESSAGE through NextWriter in its own goroutine and
// delivers the error of NextWriter, Write or Close.
func writeAsync(p *Payload, ft frame.Type, data string) <-chan error {
	res := make(chan error, 1)
	go func() {
		w, err := p.NextWriter(ft, packet.MESSAGE)
		if err == nil {
			_, err = w.Write([]byte(data))
		}
		if err == nil {
			err = w.Close()
		}
		res <- err
	}()
	return res
}

// waitWriters returns once n NextWriter calls wait for a FlushOut. The counter
// is raised just before the wait begins, so a short pause lets them block.
func waitWriters(t *testing.T, p *Payload, n int32) {
	t.Helper()
	require.Eventually(t, func() bool { return p.waiting.Load() >= n }, 5*time.Second, time.Millisecond)
	time.Sleep(20 * time.Millisecond)
}

func result(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a writer")
		return nil
	}
}

func records(body []byte) []string {
	got := strings.Split(string(body), string(separator))
	sort.Strings(got)
	return got
}

func TestFlushOutBatchesWaitingWriters(t *testing.T) {
	p := New(0, 0)
	var res []<-chan error
	for _, data := range []string{"a", "b", "c"} {
		res = append(res, writeAsync(p, frame.String, data))
	}
	res = append(res, writeAsync(p, frame.Binary, "\x01\x02\x03"))
	waitWriters(t, p, 4)

	var body bytes.Buffer
	require.NoError(t, p.FlushOut(&body))
	for _, ch := range res {
		assert.NoError(t, result(t, ch))
	}
	assert.Equal(t, []string{"4a", "4b", "4c", "bAQID"}, records(body.Bytes()))
}

func TestFlushOutHoldsPacketsThatExceedTheLimit(t *testing.T) {
	// "4aaaa" takes 5 wire bytes; two with a separator take 11.
	p := New(0, 11)
	var res []<-chan error
	for range 3 {
		res = append(res, writeAsync(p, frame.String, "aaaa"))
	}
	waitWriters(t, p, 3)

	var first, second bytes.Buffer
	require.NoError(t, p.FlushOut(&first))
	assert.Equal(t, []string{"4aaaa", "4aaaa"}, records(first.Bytes()))
	assert.LessOrEqual(t, first.Len(), 11)

	// The third writer stays blocked until a later FlushOut sends its packet.
	var open []<-chan error
	for _, ch := range res {
		select {
		case err := <-ch:
			assert.NoError(t, err)
		case <-time.After(100 * time.Millisecond):
			open = append(open, ch)
		}
	}
	require.Len(t, open, 1)

	require.NoError(t, p.FlushOut(&second))
	assert.Equal(t, "4aaaa", second.String())
	assert.NoError(t, result(t, open[0]))
}

func TestFlushOutDropsPacketLargerThanLimit(t *testing.T) {
	p := New(0, 4)
	var body bytes.Buffer
	flushed := make(chan error, 1)
	go func() { flushed <- p.FlushOut(&body) }()

	err := result(t, writeAsync(p, frame.String, "abcdef"))
	assert.ErrorIs(t, err, ErrTooLarge)

	// FlushOut kept waiting and still serves the next packet.
	require.NoError(t, result(t, writeAsync(p, frame.String, "x")))
	require.NoError(t, <-flushed)
	assert.Equal(t, "4x", body.String())
}

func TestWriterRejectsUnencodablePacket(t *testing.T) {
	p := New(0, 0)
	var body bytes.Buffer
	flushed := make(chan error, 1)
	go func() { flushed <- p.FlushOut(&body) }()

	for _, tc := range []struct {
		name string
		ft   frame.Type
		pt   packet.Type
		data string
	}{
		{"separator in text", frame.String, packet.MESSAGE, "a\x1eb"},
		{"invalid UTF-8", frame.String, packet.MESSAGE, "\xff"},
		{"binary PING", frame.Binary, packet.PING, "x"},
	} {
		w, err := p.NextWriter(tc.ft, tc.pt)
		require.NoError(t, err, tc.name)
		_, _ = w.Write([]byte(tc.data))
		assert.ErrorIs(t, w.Close(), ErrInvalidPayload, tc.name)
	}

	require.NoError(t, result(t, writeAsync(p, frame.String, "ok")))
	require.NoError(t, <-flushed)
	assert.Equal(t, "4ok", body.String())
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestFlushOutWriteErrorReachesEveryWriter(t *testing.T) {
	p := New(0, 0)
	boom := errors.New("boom")
	res := []<-chan error{writeAsync(p, frame.String, "a"), writeAsync(p, frame.String, "b")}
	waitWriters(t, p, 2)

	assert.ErrorIs(t, p.FlushOut(failingWriter{boom}), boom)
	for _, ch := range res {
		assert.ErrorIs(t, result(t, ch), boom)
	}
}

func TestFlushOutDeadlineAndClose(t *testing.T) {
	p := New(0, 0)
	require.NoError(t, p.SetWriteDeadline(time.Now().Add(20*time.Millisecond)))
	assert.EqualError(t, p.FlushOut(io.Discard), "write: timeout")

	p = New(0, 0)
	flushed := make(chan error, 1)
	go func() { flushed <- p.FlushOut(io.Discard) }()
	res := writeAsync(p, frame.String, "queued")
	assert.NoError(t, result(t, res)) // FlushOut took it

	p = New(0, 0)
	pending := writeAsync(p, frame.String, "x")
	waitWriters(t, p, 1)
	require.NoError(t, p.Close())
	assert.Equal(t, io.EOF, result(t, pending))
}

func TestFlushOutPausedWhileIdleWritesNoop(t *testing.T) {
	p := New(0, 0)
	var body bytes.Buffer
	flushed := make(chan error, 1)
	go func() { flushed <- p.FlushOut(&body) }()
	time.Sleep(20 * time.Millisecond)
	p.Pause()
	require.NoError(t, <-flushed)
	assert.Equal(t, "6", body.String())
}

func TestFlushOutPauseWaitsForAcceptedPacket(t *testing.T) {
	// A writer that holds a ticket is flushed before Pause returns.
	p := New(0, 0)
	w, err := func() (io.WriteCloser, error) {
		go func() { _ = p.FlushOut(io.Discard) }()
		return p.NextWriter(frame.String, packet.MESSAGE)
	}()
	require.NoError(t, err)

	paused := make(chan struct{})
	go func() { p.Pause(); close(paused) }()
	select {
	case <-paused:
		t.Fatal("Pause returned while a writer held a ticket")
	case <-time.After(50 * time.Millisecond):
	}
	_, _ = w.Write([]byte("m"))
	require.NoError(t, w.Close())
	waitClosed(t, paused, "Pause")
}

func TestFeedInDeliversBatchInOrder(t *testing.T) {
	p := New(0, 0)
	require.NoError(t, p.SetReadDeadline(time.Now().Add(5*time.Second)))
	fed := make(chan error, 1)
	go func() { fed <- p.FeedIn(strings.NewReader("4one\x1e2probe\x1ebAQI=\x1e6")) }()

	want := []struct {
		ft   frame.Type
		pt   packet.Type
		data string
	}{
		{frame.String, packet.MESSAGE, "one"},
		{frame.String, packet.PING, "probe"},
		{frame.Binary, packet.MESSAGE, "\x01\x02"},
		{frame.String, packet.NOOP, ""},
	}
	for i, w := range want {
		select {
		case err := <-fed:
			t.Fatalf("FeedIn returned %v before packet %d was consumed", err, i)
		default:
		}
		ft, pt, r, err := p.NextReader()
		require.NoError(t, err)
		b, err := io.ReadAll(r)
		require.NoError(t, err)
		assert.Equal(t, w.ft, ft)
		assert.Equal(t, w.pt, pt)
		assert.Equal(t, w.data, string(b))
		require.NoError(t, r.Close())
	}
	require.NoError(t, <-fed)
}

func TestFeedInTooLargeKeepsPayloadUsable(t *testing.T) {
	p := New(4, 0)
	err := p.FeedIn(strings.NewReader("4toolong"))
	assert.ErrorIs(t, err, ErrTooLarge)

	require.NoError(t, p.SetReadDeadline(time.Now().Add(5*time.Second)))
	fed := make(chan error, 1)
	go func() { fed <- p.FeedIn(strings.NewReader("4ok")) }()
	_, _, r, err := p.NextReader()
	require.NoError(t, err)
	require.NoError(t, r.Close())
	require.NoError(t, <-fed)
}

func TestFeedInMalformedFailsThePayload(t *testing.T) {
	for _, body := range []string{"", "4a\x1e", "b!!", "7x", "\x00:4a"} {
		p := New(0, 0)
		reading := make(chan error, 1)
		go func() {
			_, _, _, err := p.NextReader()
			reading <- err
		}()
		err := p.FeedIn(strings.NewReader(body))
		assert.ErrorIs(t, err, ErrInvalidPayload, "%q", body)
		assert.ErrorIs(t, <-reading, ErrInvalidPayload, "%q", body)
	}

	// A body that cannot be read fails the payload too.
	p := New(0, 0)
	boom := errors.New("boom")
	assert.ErrorIs(t, p.FeedIn(io.MultiReader(strings.NewReader("4a"), failingReader{boom})), boom)
	_, _, _, err := p.NextReader()
	assert.ErrorIs(t, err, boom)
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestFeedInDeadline(t *testing.T) {
	p := New(0, 0)
	require.NoError(t, p.SetReadDeadline(time.Now().Add(20*time.Millisecond)))
	assert.EqualError(t, p.FeedIn(strings.NewReader("4a")), "read: timeout")
	assert.False(t, p.ReadDeadline().IsZero())
}

func TestPayloadConcurrentWritersAndFeeds(t *testing.T) {
	p := New(0, 64)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = result(t, writeAsync(p, frame.String, "payload"))
		}()
	}
	delivered := 0
	for delivered < 20 {
		var body bytes.Buffer
		require.NoError(t, p.FlushOut(&body))
		assert.LessOrEqual(t, body.Len(), 64)
		delivered += len(strings.Split(body.String(), string(separator)))
	}
	wg.Wait()
}
