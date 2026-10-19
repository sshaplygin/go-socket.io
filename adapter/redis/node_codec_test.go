package redis

import (
	"context"
	"encoding/hex"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/sshaplygin/go-socket.io/adapter"
	"github.com/sshaplygin/go-socket.io/parser"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
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

type redisJSONPayload struct {
	Ignored string        `json:"-"`
	Number  uint64        `json:"renamed"`
	Binary  parser.Buffer `json:"binary"`
}
type redisCustomJSON struct{}

func (redisCustomJSON) MarshalJSON() ([]byte, error) {
	return []byte(`{"custom":9007199254740993}`), nil
}

func TestRedisCanonicalJSONAndExactNumbers(t *testing.T) {
	data, err := encodeBroadcast("peer", "/", "exact", adapter.Options{}, []interface{}{redisJSONPayload{Ignored: "secret", Number: 18446744073709551615, Binary: parser.Buffer{Data: []byte{1, 2}}}, redisCustomJSON{}})
	require.NoError(t, err)
	b, _ := isolatedRedis(t)
	target := &binaryTarget{redisTestConn: newRedisTestConn("target"), values: make(chan []interface{}, 1)}
	b.Join("r", target)
	require.NoError(t, b.onMessage("socket.io#/#", data))
	args := <-target.values
	object := args[1].(map[string]interface{})
	require.NotContains(t, object, "Ignored")
	require.Equal(t, uint64(18446744073709551615), object["renamed"])
	require.Equal(t, parser.Buffer{Data: []byte{1, 2}}, object["binary"])
	number, ok := integer(args[2].(map[string]interface{})["custom"])
	require.True(t, ok)
	require.Equal(t, int64(9007199254740993), number)
}

func TestRedisMalformedMessageValidation(t *testing.T) {
	b, _ := isolatedRedis(t)
	for _, value := range []interface{}{nil, []interface{}{}, []interface{}{1, nil, nil}, []interface{}{"peer", map[string]interface{}{"nsp": 1}, nil}, []interface{}{"peer", map[string]interface{}{"type": 2, "nsp": "/", "data": []interface{}{"event"}}, map[string]interface{}{"rooms": []interface{}{42}}}, []interface{}{"peer", map[string]interface{}{"type": 2, "nsp": "/", "data": []interface{}{"event"}}, map[string]interface{}{"flags": map[string]interface{}{"volatile": "true"}}}} {
		data, err := msgpack.Marshal(value)
		require.NoError(t, err)
		require.Error(t, b.onMessage("socket.io#/#", data))
	}
	valid, err := encodeBroadcast("local", "/", "ignored", adapter.Options{}, nil)
	require.NoError(t, err)
	require.NoError(t, b.onMessage("socket.io#/#", valid))
}

func TestRedisEmptyBinaryRemainsAttachment(t *testing.T) {
	data, err := encodeBroadcast("peer", "/", "empty", adapter.Options{}, []interface{}{parser.Buffer{}})
	require.NoError(t, err)
	var envelope []interface{}
	require.NoError(t, msgpack.Unmarshal(data, &envelope))
	raw := envelope[1].(map[string]interface{})["data"].([]interface{})[1]
	binary, ok := raw.([]byte)
	require.True(t, ok)
	require.Empty(t, binary)
}

func FuzzRedisBroadcastDecoder(f *testing.F) {
	oracle, err := os.ReadFile("testdata/node-broadcast.hex")
	if err != nil {
		f.Fatal(err)
	}
	encoded, err := hex.DecodeString(strings.TrimSpace(string(oracle)))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(encoded)
	f.Add([]byte(`{"legacy":true}`))
	f.Add([]byte{0x93, 0xc0, 0xc0, 0xc0})
	f.Fuzz(func(t *testing.T, data []byte) {
		b := &broadcast{nsp: "/chat#nested", key: "socket.io#/chat#nested#", uid: "local", Memory: adapter.NewMemory()}
		_ = b.onMessage("socket.io#/chat#nested#", data)
	})
}

func TestRedisGoPacketWithNodeOracle(t *testing.T) {
	if os.Getenv("NODE_PATH") == "" {
		t.Skip("optional oracle regeneration needs pinned Node modules; real Redis conformance is mandatory separately")
	}
	data, err := encodeBroadcast("go-peer", "/chat", "from-go", adapter.Options{Rooms: []string{"r", "s"}, Except: []string{"excluded"}, Volatile: true, NoCompress: true}, []interface{}{redisJSONPayload{Ignored: "secret", Number: 7, Binary: parser.Buffer{Data: []byte{0, 255}}}})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "node", "testdata/node-broadcast.js", hex.EncodeToString(data)).CombinedOutput()
	require.NoError(t, err, string(output))
}
