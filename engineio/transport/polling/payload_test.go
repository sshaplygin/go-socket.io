package polling

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

func TestPayloadFeedIn(t *testing.T) {
	should := assert.New(t)
	must := require.New(t)

	p := newPayload(true)
	p.Pause()
	p.Resume()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

		for _, test := range payloadVectors {
			if len(test.packets) != 1 {
				continue
			}
			r := bytes.NewReader(test.data)
			err := p.FeedIn(r, test.supportBinary)
			must.NoError(err)
		}
	}()

	for _, test := range payloadVectors {
		if len(test.packets) != 1 {
			continue
		}
		err := p.SetReadDeadline(time.Now().Add(time.Second / 10))
		require.NoError(t, err)

		ft, pt, r, err := p.NextReader()
		must.NoError(err)
		should.Equal(test.packets[0].ft, ft)
		should.Equal(test.packets[0].pt, pt)

		b, err := io.ReadAll(r)
		must.NoError(err)

		must.Nil(r.Close())

		should.Equal(test.packets[0].data, b)
	}

	err := p.SetReadDeadline(time.Now().Add(time.Second / 10))
	require.NoError(t, err)

	_, _, _, err = p.NextReader()
	should.Equal("read: timeout", err.Error())

	wg.Wait()
}

func TestPayloadFlushOutText(t *testing.T) {
	should := assert.New(t)
	must := require.New(t)

	var supportBinary bool
	p := newPayload(supportBinary)
	p.Pause()
	p.Resume()

	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()

		should := assert.New(t)
		must := require.New(t)

		for _, test := range payloadVectors {
			if len(test.packets) != 1 {
				continue
			}
			if test.supportBinary != supportBinary {
				continue
			}
			buf := bytes.NewBuffer(nil)
			err := p.FlushOut(buf)
			must.NoError(err)
			should.Equal(test.data, buf.Bytes())
		}
	}()

	for _, test := range payloadVectors {
		if len(test.packets) != 1 {
			continue
		}
		if test.supportBinary != supportBinary {
			continue
		}
		err := p.SetWriteDeadline(time.Now().Add(time.Second / 10))
		require.NoError(t, err)

		w, err := p.NextWriter(test.packets[0].ft, test.packets[0].pt)
		must.NoError(err)

		_, err = w.Write(test.packets[0].data)
		must.NoError(err)
		must.Nil(w.Close())
	}

	err := p.SetWriteDeadline(time.Now().Add(time.Second / 10))
	require.NoError(t, err)

	_, err = p.NextWriter(eioparser.Binary, eioparser.OPEN)
	should.Equal("write: timeout", err.Error())

	wg.Wait()
}

func TestPayloadFlushOutBinary(t *testing.T) {
	should := assert.New(t)
	must := require.New(t)

	var supportBinary bool
	p := newPayload(supportBinary)
	p.Pause()
	p.Resume()

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()

		should := assert.New(t)
		must := require.New(t)

		for _, test := range payloadVectors {
			if len(test.packets) != 1 {
				continue
			}
			if test.supportBinary != supportBinary {
				continue
			}

			buf := bytes.NewBuffer(nil)
			err := p.FlushOut(buf)
			must.NoError(err)
			should.Equal(test.data, buf.Bytes())
		}
	}()

	for _, test := range payloadVectors {
		if len(test.packets) != 1 {
			continue
		}
		if test.supportBinary != supportBinary {
			continue
		}

		err := p.SetWriteDeadline(time.Now().Add(time.Second / 10))
		must.NoError(err)

		w, err := p.NextWriter(test.packets[0].ft, test.packets[0].pt)
		must.NoError(err)

		_, err = w.Write(test.packets[0].data)
		must.NoError(err)

		err = w.Close()
		must.NoError(err)
	}

	err := p.SetWriteDeadline(time.Now().Add(time.Second / 10))
	must.NoError(err)

	_, err = p.NextWriter(eioparser.Binary, eioparser.OPEN)
	should.Equal("write: timeout", err.Error())

	wg.Wait()
}

func TestPayloadWaitNextClose(t *testing.T) {
	should := assert.New(t)
	must := require.New(t)

	p := newPayload(true)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

		_, _, _, err := p.NextReader()
		should.Equal(io.EOF, err)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		_, err := p.NextWriter(eioparser.Binary, eioparser.OPEN)
		should.Equal(io.EOF, err)
	}()

	// let next run
	time.Sleep(time.Second / 10)

	err := p.Close()
	must.NoError(err)

	wg.Wait()

	_, _, _, err = p.NextReader()
	should.Equal(io.EOF, err)

	_, err = p.NextWriter(eioparser.Binary, eioparser.OPEN)
	should.Equal(io.EOF, err)

	err = p.FeedIn(bytes.NewReader([]byte("1:0")), false)
	should.Equal(io.EOF, err)

	err = p.FlushOut(io.Discard)
	should.Equal(io.EOF, err)
}

func TestPayloadWaitInOutClose(t *testing.T) {
	should := assert.New(t)
	must := require.New(t)

	p := newPayload(true)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

		err := p.FeedIn(bytes.NewReader([]byte("1:0")), false)
		should.Equal(io.EOF, err)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		err := p.FlushOut(io.Discard)
		should.Equal(io.EOF, err)
	}()

	// let next run
	time.Sleep(time.Second / 10)

	must.NoError(p.Close())

	wg.Wait()

	_, _, _, err := p.NextReader()
	should.Equal(io.EOF, err)

	_, err = p.NextWriter(eioparser.Binary, eioparser.OPEN)
	should.Equal(io.EOF, err)

	err = p.FeedIn(bytes.NewReader([]byte("1:0")), false)
	should.Equal(io.EOF, err)

	err = p.FlushOut(io.Discard)
	should.Equal(io.EOF, err)
}

func TestPayloadPauseClose(t *testing.T) {
	should := assert.New(t)
	must := require.New(t)

	p := newPayload(true)
	p.Pause()

	err := p.Close()
	must.NoError(err)

	_, _, _, err = p.NextReader()
	should.Equal(io.EOF, err)

	_, err = p.NextWriter(eioparser.Binary, eioparser.OPEN)
	should.Equal(io.EOF, err)

	err = p.FeedIn(bytes.NewReader([]byte("1:0")), false)
	should.Equal(io.EOF, err)

	err = p.FlushOut(io.Discard)
	should.Equal(io.EOF, err)
}

func TestPayloadNextPause(t *testing.T) {
	should := assert.New(t)

	p := newPayload(true)

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()

		should := assert.New(t)
		must := require.New(t)

		_, _, _, err := p.NextReader()
		op, ok := err.(Error)
		must.True(ok)
		should.True(op.Temporary())
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		should := assert.New(t)
		must := require.New(t)

		_, err := p.NextWriter(eioparser.Binary, eioparser.OPEN)
		op, ok := err.(Error)
		must.True(ok)
		should.True(op.Temporary())
	}()

	// let next run
	time.Sleep(time.Second / 10)
	p.Pause()

	wg.Wait()

	_, _, _, err := p.NextReader()
	op, ok := err.(Error)
	should.True(ok)
	should.True(op.Temporary())

	_, err = p.NextWriter(eioparser.Binary, eioparser.OPEN)
	op, ok = err.(Error)
	should.True(ok)
	should.True(op.Temporary())

	err = p.FeedIn(bytes.NewReader([]byte("1:0")), false)
	op, ok = err.(Error)
	should.True(ok)
	should.True(op.Temporary())

	b := bytes.NewBuffer(nil)
	err = p.FlushOut(b)
	should.Nil(err)
	should.Equal([]byte{0x0, 0x1, 0xff, '6'}, b.Bytes())
}

func TestPayloadInOutPause(t *testing.T) {
	should := assert.New(t)
	must := require.New(t)

	p := newPayload(true)

	// feedInDone and flushOutDone are closed when FeedIn and FlushOut
	// return.
	feedInDone := make(chan struct{})
	flushOutDone := make(chan struct{})
	var flushed []byte

	go func() {
		defer close(feedInDone)

		err := p.FeedIn(bytes.NewReader([]byte("1:0")), false)
		assert.NoError(t, err)
	}()

	go func() {
		defer close(flushOutDone)

		b := bytes.NewBuffer(nil)
		err := p.FlushOut(b)
		assert.NoError(t, err)
		flushed = b.Bytes()
	}()

	// Pause only after FeedIn and FlushOut have both registered with the
	// pauser; a Pause before that makes them fail with ErrPaused instead.
	waitWorkers(t, p, 2)

	paused := make(chan struct{})
	go func() {
		p.Pause()
		close(paused)
	}()
	waitClosed(t, p.pauser.PausingTrigger(), "Pause to start")

	// FlushOut has no NextWriter to serve, so Pause makes it flush a NOOP
	// and return.
	waitClosed(t, flushOutDone, "FlushOut to return")
	should.Equal([]byte{0x0, 0x1, 0xff, '6'}, flushed)

	// FeedIn still waits for a NextReader, so Pause must keep waiting.
	select {
	case <-paused:
		t.Fatal("Pause returned while FeedIn was waiting for NextReader")
	case <-time.After(stillOpenWindow):
	}

	// NextReader still works while Pause is waiting; reading and closing
	// the frame lets FeedIn return and Pause finish.
	_, _, r, err := p.NextReader()
	must.NoError(err)
	_, err = io.Copy(io.Discard, r)
	must.NoError(err)
	must.NoError(r.Close())

	waitClosed(t, feedInDone, "FeedIn to return")
	waitClosed(t, paused, "Pause to return")

	_, _, _, err = p.NextReader()
	op, ok := err.(Error)
	should.True(ok)
	should.True(op.Temporary())

	_, err = p.NextWriter(eioparser.Binary, eioparser.OPEN)
	op, ok = err.(Error)
	should.True(ok)
	should.True(op.Temporary())

	err = p.FeedIn(bytes.NewReader([]byte("1:0")), false)
	op, ok = err.(Error)
	should.True(ok)
	should.True(op.Temporary())

	b := bytes.NewBuffer(nil)
	err = p.FlushOut(b)
	must.NoError(err)

	should.Equal([]byte{0x0, 0x1, 0xff, '6'}, b.Bytes())
}

func TestPayloadNextClosePause(t *testing.T) {
	t.Run("reader closed first", func(t *testing.T) {
		testPayloadNextClosePause(t, true)
	})
	t.Run("writer closed first", func(t *testing.T) {
		testPayloadNextClosePause(t, false)
	})
}

// stillOpenWindow is how long the test watches a Pause that must keep
// waiting. A correct Pause cannot return within it, so it only bounds how
// quickly a Pause that stops waiting too early is caught.
const stillOpenWindow = time.Second / 10

// testPayloadNextClosePause checks that Pause does not return while the
// reader returned by NextReader or the writer returned by NextWriter is
// still open. It releases them one at a time, readerFirst picking the
// order, and orders the goroutines with channels, not with sleeps.
func testPayloadNextClosePause(t *testing.T, readerFirst bool) {
	should := assert.New(t)
	must := require.New(t)

	p := newPayload(true)

	var wg sync.WaitGroup
	// readerOpen and writerOpen are closed once NextReader and NextWriter
	// have returned, so FeedIn and FlushOut are both still working.
	readerOpen := make(chan struct{})
	writerOpen := make(chan struct{})
	// releaseReader and releaseWriter let the goroutines close them;
	// the deferred calls free the goroutines if the test fails early.
	releaseReader := make(chan struct{})
	releaseWriter := make(chan struct{})
	var releaseReaderOnce, releaseWriterOnce sync.Once
	doReleaseReader := func() { releaseReaderOnce.Do(func() { close(releaseReader) }) }
	doReleaseWriter := func() { releaseWriterOnce.Do(func() { close(releaseWriter) }) }
	defer doReleaseReader()
	defer doReleaseWriter()
	// feedInDone and flushOutDone are closed when FeedIn and FlushOut
	// return, which Close of the reader and of the writer causes.
	feedInDone := make(chan struct{})
	flushOutDone := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(feedInDone)

		must := require.New(t)
		err := p.FeedIn(bytes.NewReader([]byte("1:0")), false)
		must.NoError(err)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		should := assert.New(t)
		must := require.New(t)

		_, _, r, err := p.NextReader()
		must.NoError(err)
		close(readerOpen)

		<-releaseReader
		must.Nil(r.Close())

		_, _, _, err = p.NextReader()
		op, ok := err.(Error)
		must.True(ok)
		should.True(op.Temporary())
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(flushOutDone)

		must := require.New(t)
		err := p.FlushOut(io.Discard)
		must.NoError(err)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		should := assert.New(t)
		must := require.New(t)

		w, err := p.NextWriter(eioparser.Binary, eioparser.OPEN)
		must.NoError(err)
		close(writerOpen)

		<-releaseWriter
		err = w.Close()
		must.NoError(err)

		_, err = p.NextWriter(eioparser.Binary, eioparser.OPEN)
		op, ok := err.(Error)
		must.True(ok)
		should.True(op.Temporary())
	}()

	waitClosed(t, readerOpen, "NextReader")
	waitClosed(t, writerOpen, "NextWriter")

	paused := make(chan struct{})
	go func() {
		p.Pause()
		close(paused)
	}()

	// Pause has started; it must keep waiting for the open reader and
	// writer.
	waitClosed(t, p.pauser.PausingTrigger(), "Pause to start")
	p.pauser.l.Lock()
	status := p.pauser.status
	p.pauser.l.Unlock()
	must.Equal(statusPausing, status, "Pause finished with a reader and a writer open")

	// Close one side and wait until its FeedIn or FlushOut has returned.
	// Pause must still wait for the other side.
	if readerFirst {
		doReleaseReader()
		waitClosed(t, feedInDone, "FeedIn to return")
		select {
		case <-paused:
			t.Fatal("Pause returned with the writer open")
		case <-time.After(stillOpenWindow):
		}
		doReleaseWriter()
	} else {
		doReleaseWriter()
		waitClosed(t, flushOutDone, "FlushOut to return")
		select {
		case <-paused:
			t.Fatal("Pause returned with the reader open")
		case <-time.After(stillOpenWindow):
		}
		doReleaseReader()
	}
	waitClosed(t, paused, "Pause to return")

	wg.Wait()

	_, _, _, err := p.NextReader()
	op, ok := err.(Error)
	should.True(ok)
	should.True(op.Temporary())

	_, err = p.NextWriter(eioparser.Binary, eioparser.OPEN)
	op, ok = err.(Error)
	should.True(ok)
	should.True(op.Temporary())

	err = p.FeedIn(bytes.NewReader([]byte("1:0")), false)
	op, ok = err.(Error)
	should.True(ok)
	should.True(op.Temporary())

	b := bytes.NewBuffer(nil)
	err = p.FlushOut(b)
	should.Nil(err)
	should.Equal([]byte{0x0, 0x1, 0xff, '6'}, b.Bytes())
}

// waitWorkers fails the test if the pauser of p does not count n workers
// within ten seconds.
func waitWorkers(t *testing.T, p *payload, n int) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for {
		p.pauser.l.Lock()
		worker := p.pauser.worker
		p.pauser.l.Unlock()
		if worker == n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d pauser workers, have %d", n, worker)
		}
		time.Sleep(time.Millisecond)
	}
}

// waitClosed fails the test if ch is not closed within ten seconds.
func waitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}
