package client

import (
	"bytes"

	"github.com/sshaplygin/go-socket.io/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	"testing"
)

type testStr struct {
	Result string `json:"result,omitempty"`
}

type fakeReader struct {
	data  [][]byte
	index int
	buf   *bytes.Buffer
}

func (r *fakeReader) NextReader() (parser.FrameType, io.ReadCloser, error) {
	if r.index >= len(r.data) {
		return 0, nil, io.EOF
	}
	r.buf = bytes.NewBuffer(r.data[r.index])
	ft := parser.Binary
	if r.index == 0 {
		ft = parser.Text
	}
	return ft, r, nil
}

func (r *fakeReader) Read(p []byte) (int, error) {
	return r.buf.Read(p)
}

func (r *fakeReader) Close() error {
	r.index++
	return nil
}

func TestAck(t *testing.T) {
	should := assert.New(t)
	must := require.New(t)

	namespace := "/test"
	var id uint64 = 12
	c := &conn{
		handlers:   newNamespaceRegistry(),
		namespaces: newNamespaces(),
		decoder:    parser.NewDecoder(&fakeReader{data: [][]byte{[]byte("3/test,12[{\"result\":\"pass\"}]")}}),
	}

	conn := newNamespaceConn(c, namespace)
	c.namespaces.Set(namespace, conn)

	header := parser.Header{}

	called := false
	f := newAckFunc(func(t *testStr) {
		called = true
		should.Equal("pass", t.Result)
	})
	conn.ack.Store(id, f)

	event := "a"

	err := c.decoder.DecodeHeader(&header, &event)
	must.NoError(err)
	should.Equal(parser.Ack, header.Type)
	should.Equal(12, int(header.ID))
	should.Equal("/test", header.Namespace)
	err = ackPacketHandler(c, header)
	must.NoError(err)
	must.True(called)
}

func TestClientAutomaticAcknowledgementRequiresRequestAndListener(t *testing.T) {
	p := start(t, 'C', hooks{events: map[string]interface{}{
		"value":   func(Conn) string { return "value" },
		"barrier": func(Conn) string { return "barrier" },
	}})
	p.send(t, `2["value"]`)
	p.send(t, `27["unknown"]`)
	p.send(t, `299["barrier"]`)
	require.Equal(t, "399[\"barrier\"]\n", recv(t, p.fc.out, "only requested handled ACK"))
	p.cl.Socket("/").Once("once", func(Conn) string { return "once" })
	p.send(t, `28["once"]`)
	require.Equal(t, "38[\"once\"]\n", recv(t, p.fc.out, "once ACK"))
	p.send(t, `28["once"]`)
	p.send(t, `299["barrier"]`)
	require.Equal(t, "399[\"barrier\"]\n", recv(t, p.fc.out, "consumed once listener must not ACK"))
}

func TestClientVariadicEventsAndAcknowledgements(t *testing.T) {
	p := start(t, 'C', hooks{events: map[string]interface{}{"sum": func(_ Conn, values ...int) int {
		total := 0
		for _, value := range values {
			total += value
		}
		return total
	}}})
	p.send(t, `27["sum",1,2]`)
	require.Equal(t, "37[3]\n", recv(t, p.fc.out, "variadic event ACK"))
	p.send(t, `28["sum"]`)
	require.Equal(t, "38[0]\n", recv(t, p.fc.out, "zero variadic event ACK"))
	called := make(chan int, 1)
	p.nc.Emit("question", func(values ...int) {
		total := 0
		for _, value := range values {
			total += value
		}
		called <- total
	})
	require.Equal(t, "21[\"question\"]\n", recv(t, p.fc.out, "variadic ACK request"))
	p.send(t, `31[3,4]`)
	require.Equal(t, 7, recv(t, called, "variadic ACK result"))
}
