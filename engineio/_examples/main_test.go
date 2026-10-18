package main

import (
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/sshaplygin/go-socket.io/engineio"
	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

type echoReader struct {
	io.Reader
	closed bool
	err    error
}

func (r *echoReader) Close() error { r.closed = true; return r.err }

type echoWriter struct {
	bytes.Buffer
	closed bool
	err    error
}

func (w *echoWriter) Close() error { w.closed = true; return w.err }

type echoPeer struct {
	engineio.Conn
	reader    *echoReader
	writer    *echoWriter
	writerErr error
	closeErr  error
	closed    bool
	read      bool
}

func (*echoPeer) ID() string                { return "peer" }
func (*echoPeer) LocalAddr() net.Addr       { return nil }
func (*echoPeer) RemoteAddr() net.Addr      { return nil }
func (*echoPeer) RemoteHeader() http.Header { return nil }
func (c *echoPeer) Close() error            { c.closed = true; return c.closeErr }
func (c *echoPeer) NextReader() (eioparser.FrameType, io.ReadCloser, error) {
	if c.read {
		return eioparser.String, nil, io.EOF
	}
	c.read = true
	return eioparser.String, c.reader, nil
}
func (c *echoPeer) NextWriter(eioparser.FrameType) (io.WriteCloser, error) {
	if c.writerErr != nil {
		return nil, c.writerErr
	}
	return c.writer, nil
}

type failingEchoReader struct{ err error }

func (r failingEchoReader) Read([]byte) (int, error) { return 0, r.err }

func TestEchoReleasesResourcesOnFailure(t *testing.T) {
	failure := errors.New("peer I/O failed")
	for _, tc := range []struct {
		name   string
		change func(*echoPeer)
		want   error
	}{
		{"normal EOF", func(*echoPeer) {}, io.EOF},
		{"writer acquisition", func(c *echoPeer) { c.writerErr = failure }, failure},
		{"copy", func(c *echoPeer) { c.reader.Reader = failingEchoReader{failure} }, failure},
		{"writer close", func(c *echoPeer) { c.writer.err = failure }, failure},
		{"reader close", func(c *echoPeer) { c.reader.err = failure }, failure},
		{"connection close", func(c *echoPeer) { c.closeErr = failure }, failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			peer := &echoPeer{reader: &echoReader{Reader: strings.NewReader("hello")}, writer: &echoWriter{}}
			tc.change(peer)
			err := echo(peer)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error lost: %v; want %v", err, tc.want)
			}
			if !peer.closed || !peer.reader.closed || (peer.writerErr == nil && !peer.writer.closed) {
				t.Fatalf("resources left open: conn=%v reader=%v writer=%v", peer.closed, peer.reader.closed, peer.writer.closed)
			}
			if tc.name == "normal EOF" && peer.writer.String() != "hello" {
				t.Fatalf("echo lost: %q", peer.writer.String())
			}
		})
	}
}
