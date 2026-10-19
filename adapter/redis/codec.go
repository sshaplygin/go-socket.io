package redis

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/sshaplygin/go-socket.io/adapter"
	"github.com/sshaplygin/go-socket.io/parser"
	"github.com/vmihailenco/msgpack/v5"
)

// broadcastChannel is literal; neither namespaces nor room names are split on '#'.
func (bc *broadcast) broadcastChannel() string {
	return strings.TrimSuffix(bc.key, bc.uid)
}

func (bc *broadcast) onMessage(channel string, data []byte) error {
	if !strings.HasPrefix(channel, bc.broadcastChannel()) {
		return nil
	}
	var envelope []interface{}
	if err := msgpack.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("invalid redis broadcast: %w", err)
	}
	if len(envelope) != 3 {
		return errors.New("invalid redis broadcast envelope")
	}
	uid, ok := envelope[0].(string)
	if !ok || uid == "" {
		return errors.New("invalid redis broadcast uid")
	}
	if uid == bc.uid {
		return nil
	}
	packet, ok := envelope[1].(map[string]interface{})
	if !ok {
		return errors.New("invalid redis packet")
	}
	nsp, ok := packet["nsp"].(string)
	if !ok {
		if _, exists := packet["nsp"]; exists {
			return errors.New("invalid redis namespace")
		}
		nsp = "/"
	}
	if nsp != bc.nsp {
		return nil
	}
	packetType, ok := integer(packet["type"])
	if !ok || (packetType != 2 && packetType != 5) {
		return errors.New("unsupported redis packet type")
	}
	if _, exists := packet["id"]; exists {
		return errors.New("redis broadcast acknowledgements are unsupported")
	}
	args, ok := packet["data"].([]interface{})
	if !ok || len(args) == 0 {
		return errors.New("invalid redis event data")
	}
	event, ok := args[0].(string)
	if !ok {
		return errors.New("invalid redis event name")
	}
	opts, err := decodeBroadcastOptions(envelope[2])
	if err != nil {
		return err
	}
	suffix := strings.TrimPrefix(channel, bc.broadcastChannel())
	if suffix != "" && (len(opts.Rooms) != 1 || suffix != opts.Rooms[0]+"#") {
		return nil
	}
	for i := 1; i < len(args); i++ {
		args[i] = restoreBinary(args[i])
	}
	if _, err := parser.Prepare(parser.Header{Type: parser.Event}, args); err != nil {
		return fmt.Errorf("invalid redis event payload: %w", err)
	}
	return bc.Memory.SendTo(opts, event, args[1:]...)
}

func decodeBroadcastOptions(raw interface{}) (adapter.Options, error) {
	var result adapter.Options
	object, ok := raw.(map[string]interface{})
	if !ok {
		return result, errors.New("invalid redis options")
	}
	var err error
	if rooms, exists := object["rooms"]; exists {
		result.Rooms, err = stringList(rooms)
		if err != nil {
			return result, err
		}
	}
	if except, exists := object["except"]; exists {
		result.Except, err = stringList(except)
		if err != nil {
			return result, err
		}
	}
	if rawFlags, exists := object["flags"]; exists {
		flags, ok := rawFlags.(map[string]interface{})
		if !ok {
			return result, errors.New("invalid redis flags")
		}
		for key, raw := range flags {
			flag, ok := raw.(bool)
			if !ok {
				return result, errors.New("invalid redis flag value")
			}
			switch key {
			case "volatile":
				result.Volatile = flag
			case "compress":
				result.NoCompress = !flag
			case "local":
				result.Local = flag
			}
		}
	}
	return result, nil
}
func stringList(raw interface{}) ([]string, error) {
	values, ok := raw.([]interface{})
	if !ok {
		return nil, errors.New("invalid redis room or exclusion list")
	}
	result := make([]string, len(values))
	for i, value := range values {
		text, ok := value.(string)
		if !ok {
			return nil, errors.New("invalid redis room or exclusion")
		}
		result[i] = text
	}
	return result, nil
}
func integer(value interface{}) (int64, bool) {
	switch value := value.(type) {
	case int8:
		return int64(value), true
	case int16:
		return int64(value), true
	case int32:
		return int64(value), true
	case int64:
		return value, true
	case int:
		return int64(value), true
	case uint8:
		return int64(value), true
	case uint16:
		return int64(value), true
	case uint32:
		return int64(value), true
	case uint64:
		if value <= math.MaxInt64 {
			return int64(value), true
		}
	}
	return 0, false
}
func restoreBinary(raw interface{}) interface{} {
	switch value := raw.(type) {
	case []byte:
		return parser.Buffer{Data: value}
	case []interface{}:
		for i := range value {
			value[i] = restoreBinary(value[i])
		}
	case map[string]interface{}:
		for key, item := range value {
			value[key] = restoreBinary(item)
		}
	}
	return raw
}

// do runs one command on a pooled publishing connection.
var errAdapterClosed = errors.New("redis adapter closed")

func (bc *broadcast) do(cmd string, args ...interface{}) (interface{}, error) {
	if bc.closed.Load() {
		return nil, errAdapterClosed
	}
	c := bc.pub.Get()
	defer func() { _ = c.Close() }()
	return c.Do(cmd, args...)
}

func (bc *broadcast) publish(channel string, msg interface{}) {
	resJSON, err := json.Marshal(msg)
	if err != nil {
		return
	}

	_, err = bc.do("PUBLISH", channel, resJSON)
	if err != nil {
		return
	}
}

// captureFrames obtains the canonical JSON representation and attachment bytes.
type captureFrames struct{ frames []*bytes.Buffer }
type captureWriter struct{ *bytes.Buffer }

func (captureWriter) Close() error { return nil }
func (c *captureFrames) NextWriter(parser.FrameType) (io.WriteCloser, error) {
	b := new(bytes.Buffer)
	c.frames = append(c.frames, b)
	return captureWriter{b}, nil
}

func encodeBroadcast(uid, nsp, event string, opts adapter.Options, args []interface{}) ([]byte, error) {
	if len(args) > 0 && args[len(args)-1] != nil && reflect.TypeOf(args[len(args)-1]).Kind() == reflect.Func {
		return nil, errors.New("redis broadcast acknowledgements are unsupported")
	}
	values := append([]interface{}{event}, args...)
	prepared, err := parser.Prepare(parser.Header{Type: parser.Event}, values)
	if err != nil {
		return nil, err
	}
	capture := new(captureFrames)
	if err = parser.NewEncoder(capture).EncodePrepared(prepared); err != nil {
		return nil, err
	}
	text := capture.frames[0].Bytes()
	offset := bytes.IndexByte(text, '[')
	if offset < 0 {
		return nil, errors.New("missing redis packet data")
	}
	decoder := json.NewDecoder(bytes.NewReader(text[offset:]))
	decoder.UseNumber()
	var payload interface{}
	if err = decoder.Decode(&payload); err != nil {
		return nil, err
	}
	buffers := make([][]byte, len(capture.frames)-1)
	for i := range buffers {
		buffers[i] = append([]byte{}, capture.frames[i+1].Bytes()...)
	}
	payload, err = prepareMessagePack(payload, buffers)
	if err != nil {
		return nil, err
	}
	packetType := 2
	if len(buffers) > 0 {
		packetType = 5
	}
	rooms := append([]string{}, opts.Rooms...)
	except := append([]string{}, opts.Except...)
	flags := map[string]interface{}{}
	if opts.Volatile {
		flags["volatile"] = true
	}
	if opts.NoCompress {
		flags["compress"] = false
	}
	return msgpack.Marshal([]interface{}{uid, map[string]interface{}{"type": packetType, "nsp": nsp, "data": payload}, map[string]interface{}{"rooms": rooms, "except": except, "flags": flags}})
}
func prepareMessagePack(raw interface{}, buffers [][]byte) (interface{}, error) {
	switch value := raw.(type) {
	case json.Number:
		if strings.ContainsAny(string(value), ".eE") {
			return value.Float64()
		}
		if signed, err := value.Int64(); err == nil {
			return signed, nil
		}
		return strconv.ParseUint(string(value), 10, 64)
	case []interface{}:
		for i, item := range value {
			next, err := prepareMessagePack(item, buffers)
			if err != nil {
				return nil, err
			}
			value[i] = next
		}
	case map[string]interface{}:
		if placeholder, ok := value["_placeholder"].(bool); ok && placeholder {
			number, ok := value["num"].(json.Number)
			if !ok || len(value) != 2 {
				return nil, errors.New("invalid binary placeholder")
			}
			index, err := strconv.ParseUint(string(number), 10, 64)
			if err != nil || index >= uint64(len(buffers)) {
				return nil, errors.New("invalid binary attachment index")
			}
			return buffers[index], nil
		}
		for key, item := range value {
			next, err := prepareMessagePack(item, buffers)
			if err != nil {
				return nil, err
			}
			value[key] = next
		}
	}
	return raw, nil
}
