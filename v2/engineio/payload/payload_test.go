package payload

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/v2/engineio/frame"
	"github.com/sshaplygin/go-socket.io/v2/engineio/packet"
)

func TestPayloadWaitNextClose(t *testing.T) {
	should := assert.New(t)
	must := require.New(t)

	p := New(0, 0)

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

		_, err := p.NextWriter(frame.String, packet.OPEN)
		should.Equal(io.EOF, err)
	}()

	// let next run
	time.Sleep(time.Second / 10)

	err := p.Close()
	must.NoError(err)

	wg.Wait()

	_, _, _, err = p.NextReader()
	should.Equal(io.EOF, err)

	_, err = p.NextWriter(frame.String, packet.OPEN)
	should.Equal(io.EOF, err)

	err = p.FeedIn(bytes.NewReader([]byte("0")))
	should.Equal(io.EOF, err)

	err = p.FlushOut(io.Discard)
	should.Equal(io.EOF, err)
}

func TestPayloadWaitInOutClose(t *testing.T) {
	should := assert.New(t)
	must := require.New(t)

	p := New(0, 0)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

		err := p.FeedIn(bytes.NewReader([]byte("0")))
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

	_, err = p.NextWriter(frame.String, packet.OPEN)
	should.Equal(io.EOF, err)

	err = p.FeedIn(bytes.NewReader([]byte("0")))
	should.Equal(io.EOF, err)

	err = p.FlushOut(io.Discard)
	should.Equal(io.EOF, err)
}

func TestPayloadPauseClose(t *testing.T) {
	should := assert.New(t)
	must := require.New(t)

	p := New(0, 0)
	p.Pause()

	err := p.Close()
	must.NoError(err)

	_, _, _, err = p.NextReader()
	should.Equal(io.EOF, err)

	_, err = p.NextWriter(frame.String, packet.OPEN)
	should.Equal(io.EOF, err)

	err = p.FeedIn(bytes.NewReader([]byte("0")))
	should.Equal(io.EOF, err)

	err = p.FlushOut(io.Discard)
	should.Equal(io.EOF, err)
}

func TestPayloadNextPause(t *testing.T) {
	should := assert.New(t)

	p := New(0, 0)

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

		_, err := p.NextWriter(frame.String, packet.OPEN)
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

	_, err = p.NextWriter(frame.String, packet.OPEN)
	op, ok = err.(Error)
	should.True(ok)
	should.True(op.Temporary())

	err = p.FeedIn(bytes.NewReader([]byte("0")))
	op, ok = err.(Error)
	should.True(ok)
	should.True(op.Temporary())

	b := bytes.NewBuffer(nil)
	err = p.FlushOut(b)
	should.Nil(err)
	should.Equal([]byte("6"), b.Bytes())
}

func TestPayloadInOutPause(t *testing.T) {
	should := assert.New(t)
	must := require.New(t)

	p := New(0, 0)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()

		err := p.FeedIn(bytes.NewReader([]byte("0")))
		must.NoError(err)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()

		b := bytes.NewBuffer(nil)
		err := p.FlushOut(b)
		must.NoError(err)

		should.Equal([]byte("6"), b.Bytes())
	}()

	go func() {
		time.Sleep(time.Second / 10 * 3)

		_, _, r, err := p.NextReader()
		defer func() {
			must.NoError(r.Close())
		}()
		must.NoError(err)

		_, err = io.Copy(io.Discard, r)
		must.NoError(err)
	}()

	//wait other run
	time.Sleep(time.Second / 10)

	start := time.Now()
	p.Pause()
	end := time.Now()
	should.True(end.Sub(start) >= time.Second/10)

	wg.Wait()

	_, _, _, err := p.NextReader()
	op, ok := err.(Error)
	should.True(ok)
	should.True(op.Temporary())

	_, err = p.NextWriter(frame.String, packet.OPEN)
	op, ok = err.(Error)
	should.True(ok)
	should.True(op.Temporary())

	err = p.FeedIn(bytes.NewReader([]byte("0")))
	op, ok = err.(Error)
	should.True(ok)
	should.True(op.Temporary())

	b := bytes.NewBuffer(nil)
	err = p.FlushOut(b)
	must.NoError(err)

	should.Equal([]byte("6"), b.Bytes())
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

	p := New(0, 0)

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
		err := p.FeedIn(bytes.NewReader([]byte("0")))
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

		w, err := p.NextWriter(frame.String, packet.OPEN)
		must.NoError(err)
		close(writerOpen)

		<-releaseWriter
		err = w.Close()
		must.NoError(err)

		_, err = p.NextWriter(frame.String, packet.OPEN)
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

	_, err = p.NextWriter(frame.String, packet.OPEN)
	op, ok = err.(Error)
	should.True(ok)
	should.True(op.Temporary())

	err = p.FeedIn(bytes.NewReader([]byte("0")))
	op, ok = err.(Error)
	should.True(ok)
	should.True(op.Temporary())

	b := bytes.NewBuffer(nil)
	err = p.FlushOut(b)
	should.Nil(err)
	should.Equal([]byte("6"), b.Bytes())
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
