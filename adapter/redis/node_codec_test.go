package redis

import (
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/sshaplygin/go-socket.io/adapter"
	"github.com/sshaplygin/go-socket.io/parser"
	"github.com/stretchr/testify/require"
)

type binaryTarget struct {
	*redisTestConn
	values chan []interface{}
	flags  chan adapter.Options
}

func (c *binaryTarget) Emit(name string, args ...interface{}) {
	c.values <- append([]interface{}{name}, args...)
}
func (c *binaryTarget) EmitWithOptions(opts adapter.Options, name string, args ...interface{}) {
	c.flags <- opts
	c.Emit(name, args...)
}

// Covers A4
// Covers B3
// Covers S21
func TestRedisNodeBroadcastOracle(t *testing.T) {
	b, _ := isolatedRedis(t)
	b.nsp = "/chat#nested"
	b.key = "socket.io#/chat#nested#local"
	target := &binaryTarget{redisTestConn: newRedisTestConn("allowed"), values: make(chan []interface{}, 2), flags: make(chan adapter.Options, 2)}
	excluded := newRedisTestConn("excluded")
	for _, room := range []string{"r", "s"} {
		b.Join(room, target)
		b.Join(room, excluded)
	}
	encoded, err := os.ReadFile("testdata/node-broadcast.hex")
	require.NoError(t, err)
	data, err := hex.DecodeString(strings.TrimSpace(string(encoded)))
	require.NoError(t, err)
	require.NoError(t, b.onMessage("socket.io#/chat#nested#", data))
	select {
	case args := <-target.values:
		require.Equal(t, "binary", args[0])
		object := args[1].(map[string]interface{})
		require.Equal(t, parser.Buffer{Data: []byte{0, 255}}, object["bin"])
		require.Equal(t, parser.Buffer{Data: []byte{1, 254}}, object["nested"].([]interface{})[0])
	default:
		t.Fatal("Node MessagePack broadcast was not delivered")
	}
	require.Empty(t, target.values, "room union must deliver once")
	require.Empty(t, excluded.events)
	opts := <-target.flags
	require.True(t, opts.Volatile)
	require.True(t, opts.NoCompress)
}

// Covers A4
// Covers B3
func TestRedisPublicationErrorsAndUnsupportedCallbacks(t *testing.T) {
	b, calls := isolatedRedis(t)
	target := newRedisTestConn("target")
	b.Join("r", target)
	require.Error(t, b.SendTo(adapter.Options{}, "callback", func() {}))
	require.Empty(t, target.events)
	require.Zero(t, calls.Load())
	require.Error(t, b.SendTo(adapter.Options{}, "bad", make(chan int)))
	require.Empty(t, target.events)
	require.Error(t, b.SendTo(adapter.Options{}, "publish-error", 1))
}

// Covers A4
// Covers S13
func TestRedisRoomUnionAndExclusionAcrossInstances(t *testing.T) {
	s := miniredis.RunT(t)
	a := newTestRedisBroadcast(t, s)
	b := newTestRedisBroadcast(t, s)
	target := newRedisTestConn("target")
	excluded := newRedisTestConn("excluded")
	for _, room := range []string{"r", "s"} {
		b.Join(room, target)
		b.Join(room, excluded)
	}
	require.NoError(t, a.SendTo(adapter.Options{Rooms: []string{"r", "s"}, Except: []string{"excluded"}}, "union", 42))
	expectEvent(t, target, "union [42]")
	require.Empty(t, target.events)
	require.Empty(t, excluded.events)
}
