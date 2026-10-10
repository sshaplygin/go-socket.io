package transport

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConnParameters(t *testing.T) {
	must := require.New(t)
	at := assert.New(t)

	tests := []struct {
		para ConnParameters
		out  string
	}{
		{
			ConnParameters{
				PingInterval: time.Second * 10,
				PingTimeout:  time.Second * 5,
				SID:          "vCcJKmYQcIf801WDAAAB",
				Upgrades:     []string{"websocket", "polling"},
			},
			"{\"sid\":\"vCcJKmYQcIf801WDAAAB\",\"upgrades\":[\"websocket\",\"polling\"],\"pingInterval\":10000,\"pingTimeout\":5000}\n",
		},
		{
			ConnParameters{
				PingInterval: time.Second * 25,
				PingTimeout:  time.Second * 20,
				SID:          "lv_VI97HAXpY6yYWAAAC",
				Upgrades:     []string{"websocket"},
				MaxPayload:   1000000,
			},
			"{\"sid\":\"lv_VI97HAXpY6yYWAAAC\",\"upgrades\":[\"websocket\"],\"pingInterval\":25000,\"pingTimeout\":20000,\"maxPayload\":1000000}\n",
		},
	}
	for _, test := range tests {
		buf := bytes.NewBuffer(nil)
		n, err := test.para.WriteTo(buf)
		must.Nil(err)

		at.Equal(int64(len(test.out)), n)
		at.Equal(test.out, buf.String())

		conn, err := ReadConnParameters(buf)
		must.Nil(err)
		at.Equal(test.para, conn)
	}
}

func BenchmarkConnParameters(b *testing.B) {
	must := require.New(b)

	param := ConnParameters{
		PingInterval: time.Second * 10,
		PingTimeout:  time.Second * 5,
		SID:          "vCcJKmYQcIf801WDAAAB",
		Upgrades:     []string{"websocket", "polling"},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := param.WriteTo(io.Discard)
		must.Nil(err)
	}
}
