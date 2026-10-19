package client

import (
	"encoding/json"
	"errors"
	"github.com/sshaplygin/go-socket.io/parser"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestClientEmitSnapshotsQueuedAndBufferedValues(t *testing.T) {
	t.Run("connected", func(t *testing.T) {
		p := start(t, 'C', hooks{})
		p.fc.hold.Store(true)
		p.nc.Emit("blocker")
		recv(t, p.fc.held, "blocked writer")
		value := map[string]string{"value": "before"}
		p.nc.Emit("snapshot", value)
		value["value"] = "after"
		close(p.fc.release)
		require.Equal(t, ev("blocker"), recv(t, p.fc.out, "blocker"))
		require.Equal(t, ev("snapshot", map[string]string{"value": "before"}), recv(t, p.fc.out, "snapshot"))
	})
	t.Run("before connect", func(t *testing.T) {
		p := newPeer(t, 'C', hooks{})
		value := map[string]string{"value": "before"}
		p.cl.Socket("/").Emit("snapshot", value)
		value["value"] = "after"
		p.connect(t)
		require.Equal(t, ev("snapshot", map[string]string{"value": "before"}), recv(t, p.fc.out, "buffered snapshot"))
	})
}

type clientReentrantJSON struct {
	check    func()
	timedOut *bool
}

func (v clientReentrantJSON) MarshalJSON() ([]byte, error) {
	done := make(chan struct{})
	go func() { v.check(); close(done) }()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		*v.timedOut = true
	}
	return json.Marshal("snapshot")
}

func TestClientSnapshotMarshalerRunsOutsideManagerLocks(t *testing.T) {
	p := start(t, 'C', hooks{})
	timedOut := false
	p.nc.Emit("snapshot", clientReentrantJSON{check: func() { p.cl.Socket("/").Connected() }, timedOut: &timedOut})
	require.Equal(t, ev("snapshot", "snapshot"), recv(t, p.fc.out, "custom JSON"))
	require.False(t, timedOut, "custom JSON must run before manager locks")
}

func TestClientEmitSnapshotsBinaryAttachments(t *testing.T) {
	for _, connected := range []bool{false, true} {
		t.Run(map[bool]string{false: "buffered", true: "connected"}[connected], func(t *testing.T) {
			p := newPeer(t, 'C', hooks{})
			if connected {
				p.connect(t)
				p.fc.hold.Store(true)
				p.nc.Emit("blocker")
				recv(t, p.fc.held, "blocked writer")
			}
			value := parser.Buffer{Data: []byte("before")}
			p.cl.Socket("/").Emit("binary", value)
			copy(value.Data, "after!")
			if !connected {
				p.connect(t)
			} else {
				close(p.fc.release)
				require.Equal(t, ev("blocker"), recv(t, p.fc.out, "blocker"))
			}
			require.Equal(t, "51-[\"binary\",{\"_placeholder\":true,\"num\":0}]\n", recv(t, p.fc.out, "binary header"))
			require.Equal(t, "before", recv(t, p.fc.out, "binary snapshot"))
		})
	}
}

func TestClientConnectPanicReportsAndCloses(t *testing.T) {
	want := errors.New("connect callback failed")
	p := newPeer(t, 'C', hooks{connect: func(Conn) error { panic(want) }})
	require.NoError(t, p.cl.Connect())
	closeAtEnd(t, p.fc, p.conn())
	p.send(t, "0")
	require.ErrorIs(t, recv(t, p.errs, "connect panic report").err, want)
	recv(t, p.fc.closed, "connection closed after connect panic")
}
