package payload

import (
	"bytes"
	"io"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
	"github.com/stretchr/testify/require"
)

func waitFlushing(t *testing.T, p *Payload) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for atomic.LoadInt32(&p.flushing) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("FlushOut never started")
		}
		runtime.Gosched()
	}
}

func TestPayloadAnswerPoll(t *testing.T) {
	for _, binary := range []bool{false, true} {
		p := New(binary)
		require.False(t, p.AnswerPoll())
		var b bytes.Buffer
		flushed := make(chan error, 1)
		go func() { flushed <- p.FlushOut(&b) }()
		require.Eventually(t, p.AnswerPoll, time.Second, time.Millisecond)
		require.NoError(t, <-flushed)
		require.Equal(t, p.encoder.NOOP(), b.Bytes())

		b.Reset()
		go func() { flushed <- p.FlushOut(&b) }()
		w, err := p.NextWriter(frame.String, packet.MESSAGE)
		require.NoError(t, err)
		require.False(t, p.AnswerPoll(), "a session writer already owns this response")
		_, err = io.WriteString(w, "data")
		require.NoError(t, err)
		require.NoError(t, w.Close())
		require.NoError(t, <-flushed)
		if binary {
			require.Equal(t, []byte{0, 5, 255, '4', 'd', 'a', 't', 'a'}, b.Bytes())
		} else {
			require.Equal(t, "5:4data", b.String())
		}
		p.Pause()
		require.False(t, p.AnswerPoll())
		p.Resume()
		require.NoError(t, p.Close())
		require.False(t, p.AnswerPoll())
	}
	for i := 0; i < 100; i++ {
		p := New(false)
		var b bytes.Buffer
		flushed := make(chan error, 1)
		writerDone := make(chan bool, 1)
		answerDone := make(chan bool, 1)
		go func() { flushed <- p.FlushOut(&b) }()
		waitFlushing(t, p)
		start := make(chan struct{})
		go func() { <-start; answerDone <- p.AnswerPoll() }()
		go func() {
			<-start
			w, err := p.NextWriter(frame.String, packet.MESSAGE)
			if err != nil {
				writerDone <- false
				return
			}
			_, err = io.WriteString(w, "data")
			if err == nil {
				err = w.Close()
			}
			writerDone <- err == nil
		}()
		close(start)
		select {
		case err := <-flushed:
			require.NoError(t, err)
		case <-time.After(time.Second):
			t.Fatal("neither contender answered the poll")
		}
		require.NoError(t, p.Close())
		answered, written := <-answerDone, <-writerDone
		require.NotEqual(t, answered, written, "exactly one contender must own the response")
		if answered {
			require.Equal(t, "1:6", b.String())
		} else {
			require.Equal(t, "5:4data", b.String())
		}
	}
}
