package polling

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

func TestOpError(t *testing.T) {
	assert := assert.New(t)

	payloadVectors := []struct {
		op        string
		err       error
		temporary bool
		errString string
	}{
		{"read", errPaused, true, "read: paused"},
		{"read", errTimeout, false, "read: timeout"},
	}

	for _, test := range payloadVectors {
		var err = newOpError(test.op, test.err)

		assert.EqualError(err, test.errString)

		re, ok := err.(Error)
		assert.True(ok)

		assert.Equal(test.temporary, re.Temporary())
	}
}

// TestPayloadTimeoutReportsTimeout checks that an expired payload deadline is an
// *OpError reporting Timeout, on which the session reports "ping timeout", with
// its text unchanged and not temporary, so that the session does not retry it.
func TestPayloadTimeoutReportsTimeout(t *testing.T) {
	past := time.Now().Add(-time.Millisecond)
	for _, op := range []string{"read", "write"} {
		t.Run(op, func(t *testing.T) {
			p := newPayload(true)
			defer func() { _ = p.Close() }()
			var err error
			if op == "read" {
				require.NoError(t, p.SetReadDeadline(past))
				_, _, _, err = p.NextReader()
			} else {
				require.NoError(t, p.SetWriteDeadline(past))
				_, err = p.NextWriter(eioparser.String, eioparser.MESSAGE)
			}
			require.IsType(t, &OpError{}, err)
			timeout, ok := err.(interface{ Timeout() bool })
			require.True(t, ok, "%T has no Timeout method", err)
			require.True(t, timeout.Timeout())
			require.EqualError(t, err, op+": timeout")
			var payloadErr Error
			require.ErrorAs(t, err, &payloadErr)
			require.False(t, payloadErr.Temporary())
		})
	}
}

// TestDetachedWriterReportsStoredError holds a poll response in a session writer
// while the poll ends. Its Close then reports what ended the poll: the expired
// write deadline as a timeout, a cancelled poll as context.Canceled, and a closed
// payload as no timeout.
func TestDetachedWriterReportsStoredError(t *testing.T) {
	for _, end := range []string{"deadline", "cancel", "close"} {
		t.Run(end, func(t *testing.T) {
			p := newPayload(false)
			defer func() { _ = p.Close() }()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			deadline := time.Now().Add(time.Minute)
			if end == "deadline" {
				deadline = time.Now().Add(100 * time.Millisecond)
			}
			require.NoError(t, p.SetWriteDeadline(deadline))
			done := make(chan error, 1)
			go func() { done <- p.flushOut(ctx, io.Discard) }()
			w, err := p.NextWriter(eioparser.String, eioparser.MESSAGE)
			require.NoError(t, err)
			_, err = w.Write([]byte("hello"))
			require.NoError(t, err)
			switch end {
			case "cancel":
				cancel()
			case "close":
				require.NoError(t, p.Close())
			}
			require.Error(t, <-done) // the poll ends with write: timeout, context canceled or EOF
			err = w.Close()
			require.Error(t, err)
			var timeout interface{ Timeout() bool }
			require.Equal(t, end == "deadline", errors.As(err, &timeout) && timeout.Timeout(), "%v", err)
			if end == "cancel" {
				var op *OpError
				require.ErrorAs(t, err, &op)
				require.ErrorIs(t, op.Err, context.Canceled)
			}
		})
	}
}
