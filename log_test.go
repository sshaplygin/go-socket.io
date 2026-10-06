package socketio

import (
	"errors"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sshaplygin/go-socket.io/engineio"
	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
	"github.com/sshaplygin/go-socket.io/engineio/transport"
	"github.com/sshaplygin/go-socket.io/engineio/transport/polling"
	"github.com/sshaplygin/go-socket.io/engineio/transport/websocket"
)

// captureLogs sends the records of opts.Logger and of slog.Default to one recorder. It
// must not run in parallel; Cleanup restores the default logger.
func captureLogs(t *testing.T, opts *engineio.Options) *attrRecorder {
	rec := newAttrRecorder()
	prev, prevOut, prevFlags := slog.Default(), log.Writer(), log.Flags()
	slog.SetDefault(slog.New(rec))
	t.Cleanup(func() { slog.SetDefault(prev); log.SetOutput(prevOut); log.SetFlags(prevFlags) })
	opts.Logger = slog.New(rec)
	return rec
}

// since returns the records kept after the first n.
func (h *attrRecorder) since(n int) []map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]map[string]string(nil), (*h.recs)[n:]...)
}

func (h *attrRecorder) len() int { return len(h.since(0)) }

// loud returns the records that the module's code logged above DEBUG, other than msg.
func loud(recs []map[string]string, msg string) (out []map[string]string) {
	for _, m := range recs {
		if strings.HasPrefix(m["func"], "github.com/sshaplygin/go-socket.io") && m["msg"] != msg &&
			m["level"] != slog.LevelDebug.String() && m["level"] != "DEBUG-4" {
			out = append(out, m)
		}
	}
	return out
}

func only(recs []map[string]string, msg string) (out []map[string]string) {
	for _, m := range recs {
		if m["msg"] == msg {
			out = append(out, m)
		}
	}
	return out
}

// faultyTransport accepts connections whose writes fail, so that InitSession fails.
type faultyTransport struct{ transport.Transport }

type faultyConn struct{ transport.Conn }

func (faultyTransport) Name() string { return "faulty" }
func (faultyTransport) Accept(http.ResponseWriter, *http.Request) (transport.Conn, error) {
	return faultyConn{}, nil
}
func (faultyConn) SetReadDeadline(time.Time) error  { return nil }
func (faultyConn) SetWriteDeadline(time.Time) error { return nil }
func (faultyConn) Close() error                     { return nil }
func (faultyConn) RemoteAddr() net.Addr             { return nil }
func (faultyConn) NextWriter(frame.Type, packet.Type) (io.WriteCloser, error) {
	return nil, errors.New("write failed")
}

// Covers 1L-T9 (S).
func TestRequestRejectedRecords(t *testing.T) {
	opts := &engineio.Options{
		Transports: []transport.Transport{polling.Default, websocket.Default, faultyTransport{}},
		RequestChecker: func(r *http.Request) (http.Header, error) {
			if r.Header.Get("X-Reject") != "" {
				return nil, errors.New("rejected")
			}
			return nil, nil
		},
	}
	rec := captureLogs(t, opts)
	srv := NewServer(opts)
	srv.OnConnect("/", func(Conn) error { return nil })
	go func() { _ = srv.Serve() }()
	t.Cleanup(func() { _ = srv.Close() })
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	get := func(query, ctype string, header ...string) int {
		req, err := http.NewRequest(http.MethodGet, ts.URL+"/?EIO=3&"+query, strings.NewReader("1:2"))
		require.NoError(t, err)
		if ctype != "" {
			req.Method = http.MethodPost
			req.Header.Set("Content-Type", ctype)
		}
		for i := 0; i < len(header); i += 2 {
			req.Header.Set(header[i], header[i+1])
		}
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		_, _ = io.Copy(io.Discard, resp.Body)
		require.NoError(t, resp.Body.Close())
		return resp.StatusCode
	}
	dial := func(tr transport.Transport) string {
		u, err := url.Parse(ts.URL + "/?EIO=3")
		require.NoError(t, err)
		c, err := tr.Dial(u, nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = c.Close() })
		if o, ok := c.(engineio.Opener); ok {
			params, err := o.Open()
			require.NoError(t, err)
			return params.SID
		}
		_, _, r, err := c.NextReader()
		require.NoError(t, err)
		params, err := transport.ReadConnParameters(r)
		require.NoError(t, err)
		return params.SID
	}
	wsSID, pollingSID := dial(websocket.Default), dial(polling.Default)

	for _, tc := range []struct {
		reason, level string
		do            func() int
		status        int
	}{
		{"bad transport", "WARN", func() int { return get("transport=foo", "") }, http.StatusBadRequest},
		{"checker", "WARN", func() int { return get("transport=polling", "", "X-Reject", "1") }, http.StatusBadGateway},
		{"unknown sid", "DEBUG", func() int { return get("transport=polling&sid=nope", "") }, http.StatusBadRequest},
		{"accept", "WARN", func() int { return get("transport=websocket", "") }, http.StatusBadRequest},
		{"init", "WARN", func() int { return get("transport=faulty", "") }, http.StatusOK},
		{"bad upgrade", "WARN", func() int { return get("transport=polling&sid="+wsSID, "") }, http.StatusBadRequest},
		{"", "", func() int { return get("transport=polling&sid="+pollingSID, "application/bogus") }, http.StatusBadRequest},
	} {
		n := rec.len()
		require.Equal(t, tc.status, tc.do(), tc.reason)
		var rejected []map[string]string
		require.Eventually(t, func() bool {
			rejected = only(rec.since(n), "engineio: request rejected")
			return len(rejected) > 0 || tc.reason == ""
		}, waitFor, time.Millisecond, "no record for %q", tc.reason)
		time.Sleep(20 * time.Millisecond) // records the trigger might log late
		recs := rec.since(n)
		if tc.reason != "" {
			require.Len(t, only(recs, "engineio: request rejected"), 1, tc.reason)
			require.Equal(t, tc.reason, rejected[0]["reason"])
			require.Equal(t, tc.level, rejected[0]["level"], tc.reason)
		}
		require.Empty(t, loud(recs, "engineio: request rejected"), tc.reason)
		require.Empty(t, only(recs, "engineio: session open"), tc.reason)
		require.Empty(t, only(recs, "engineio: session close"), tc.reason)
		for _, m := range recs {
			require.NotContains(t, m["msg"], "superfluous", tc.reason)
		}
	}
}
