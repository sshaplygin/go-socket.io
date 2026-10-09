package codec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/sshaplygin/go-socket.io/parser"
	"github.com/vmihailenco/msgpack/v5"
)

// maxEncodeDepth bounds the nesting of a packet's JSON data on encode. A peer's
// decoder applies its own Limits.MaxDepth.
const maxEncodeDepth = MaxDepthCeiling

// Broadcast is the message a node publishes to deliver a packet to the sockets of its
// peers: the MessagePack array [uid, packet, opts] of the Node adapter. UID is the
// publisher's server id, which a receiver compares with its own to ignore its echo.
//
// Packet.Type holds the base type (Event, Ack, ...); on the wire a packet has the
// base type and its binary values are MessagePack bin values inside the data, as
// the Node adapter publishes them, not the BINARY_EVENT framing of the Socket.IO
// stream. In Packet, Data holds the JSON of the data with a
// {"_placeholder":true,"num":N} object for each binary value, and Attachments[N]
// holds its bytes.
type Broadcast struct {
	UID     string
	Packet  parser.Packet
	Options Options
}

type wirePacket struct {
	Type *int            `json:"type"`
	Data json.RawMessage `json:"data"`
	Nsp  *string         `json:"nsp"`
	ID   *uint64         `json:"id"`
}

type wireOptions struct {
	Rooms  []string `json:"rooms"`
	Except []string `json:"except"`
	Flags  *Flags   `json:"flags"`
}

// DecodeBroadcast decodes a message published to a broadcast channel. The result is
// owned by the caller. The packet type 5 and 6 of the Socket.IO stream are accepted
// and mapped to Event and Ack. An absent nsp is "/", as in the Node adapter.
//
// The input is untrusted: it is bounded by lim, and a peer's snapshot or packet is
// not otherwise validated (event names, namespace existence and room names are the
// adapter's business).
func DecodeBroadcast(msg []byte, lim Limits) (Broadcast, error) {
	lim, err := lim.resolve()
	if err != nil {
		return Broadcast{}, err
	}
	if err := lim.checkSize(msg); err != nil {
		return Broadcast{}, err
	}
	r := &mpReader{b: msg, lim: lim}
	n, err := r.readArrayLen()
	if err != nil {
		return Broadcast{}, err
	}
	if n != 3 {
		return Broadcast{}, fmt.Errorf("%w: broadcast array has %d elements, want 3", ErrMalformed, n)
	}
	uid, err := r.readString()
	if err != nil {
		return Broadcast{}, err
	}
	if uid == "" {
		return Broadcast{}, fmt.Errorf("%w: empty uid", ErrMalformed)
	}

	r.allowBinary = true
	if err := r.value(1); err != nil {
		return Broadcast{}, err
	}
	var wp wirePacket
	if err := json.Unmarshal(r.out, &wp); err != nil {
		return Broadcast{}, fmt.Errorf("%w: packet: %v", ErrMalformed, err)
	}
	atts := r.atts

	r.allowBinary, r.out, r.atts = false, r.out[:0], nil
	if err := r.value(1); err != nil {
		return Broadcast{}, err
	}
	var wo wireOptions
	if err := json.Unmarshal(r.out, &wo); err != nil {
		return Broadcast{}, fmt.Errorf("%w: opts: %v", ErrMalformed, err)
	}
	if r.pos != len(msg) {
		return Broadcast{}, fmt.Errorf("%w: %d bytes after the message", ErrMalformed, len(msg)-r.pos)
	}

	pkt, err := packetFromWire(wp, atts)
	if err != nil {
		return Broadcast{}, err
	}
	return Broadcast{UID: uid, Packet: pkt, Options: Options(wo)}, nil
}

// checkPlaceholders requires that the data holds each of the n placeholders the reader
// wrote exactly once. A binary value under another key, or one in a key that a later
// duplicate replaced, leaves an attachment without a placeholder, which the encoder
// refuses. The reader writes the placeholder in one fixed form, and a string cannot
// contain it because quotes inside strings are escaped.
func checkPlaceholders(data []byte, n int) error {
	for i := 0; i < n; i++ {
		needle := `{"_placeholder":true,"num":` + strconv.Itoa(i) + `}`
		if c := bytes.Count(data, []byte(needle)); c != 1 {
			return fmt.Errorf("%w: attachment %d has %d placeholders in the data, want 1", ErrMalformed, i, c)
		}
	}
	return nil
}

func packetFromWire(wp wirePacket, atts [][]byte) (parser.Packet, error) {
	if wp.Type == nil {
		return parser.Packet{}, fmt.Errorf("%w: packet has no type", ErrMalformed)
	}
	var t parser.Type
	switch {
	case *wp.Type >= 0 && *wp.Type <= int(parser.Error):
		t = parser.Type(*wp.Type)
	case *wp.Type == 5: // BINARY_EVENT
		t = parser.Event
	case *wp.Type == 6: // BINARY_ACK
		t = parser.Ack
	default:
		return parser.Packet{}, fmt.Errorf("%w: packet type %d", ErrMalformed, *wp.Type)
	}
	pkt := parser.Packet{Type: t, Namespace: "/", ID: wp.ID}
	if wp.Nsp != nil {
		pkt.Namespace = *wp.Nsp
	}
	if len(wp.Data) > 0 && string(wp.Data) != "null" {
		pkt.Data = wp.Data
	}
	if len(atts) > 0 {
		if err := checkPlaceholders(pkt.Data, len(atts)); err != nil {
			return parser.Packet{}, err
		}
		pkt.Attachments = atts
	}
	if (t == parser.Event || t == parser.Ack) && (len(pkt.Data) == 0 || pkt.Data[0] != '[') {
		return parser.Packet{}, fmt.Errorf("%w: event or ack packet without an array as data", ErrMalformed)
	}
	return pkt, nil
}

// EncodeBroadcast encodes b the way the Node adapter does: the key order of the
// packet is type, data, nsp, id; integers use the smallest MessagePack encoding.
// An empty Packet.Namespace is written as "/". Rooms and Except are written as arrays
// even when empty, and a nil Options.Flags as an empty object.
//
// It refuses (ErrInvalid) an empty UID, a packet type above Error, an Event or Ack
// whose Data is not a JSON array, Data that is not valid JSON, and attachments that
// do not match the placeholders one to one.
func EncodeBroadcast(b Broadcast) ([]byte, error) {
	if b.UID == "" {
		return nil, fmt.Errorf("%w: empty uid", ErrInvalid)
	}
	p := b.Packet
	if p.Type > parser.Error {
		return nil, fmt.Errorf("%w: packet type %d", ErrInvalid, p.Type)
	}
	var data *jsonNode
	if len(p.Data) > 0 {
		var err error
		if data, err = parseJSON(p.Data, maxEncodeDepth); err != nil {
			return nil, err
		}
	}
	if (p.Type == parser.Event || p.Type == parser.Ack) && (data == nil || data.kind != '[') {
		return nil, fmt.Errorf("%w: event or ack packet without an array as data", ErrInvalid)
	}

	var buf bytes.Buffer
	enc := msgpack.NewEncoder(&buf)
	enc.UseCompactInts(true)
	w := &mpWriter{enc: enc, atts: p.Attachments, used: make([]bool, len(p.Attachments))}

	if err := enc.EncodeArrayLen(3); err != nil {
		return nil, err
	}
	if err := enc.EncodeString(b.UID); err != nil {
		return nil, err
	}

	entries := 2
	if data != nil {
		entries++
	}
	if p.ID != nil {
		entries++
	}
	if err := enc.EncodeMapLen(entries); err != nil {
		return nil, err
	}
	if err := encodeKV(enc, "type", func() error { return enc.EncodeUint(uint64(p.Type)) }); err != nil {
		return nil, err
	}
	if data != nil {
		if err := encodeKV(enc, "data", func() error { return w.node(data) }); err != nil {
			return nil, err
		}
	}
	nsp := p.Namespace
	if nsp == "" {
		nsp = "/"
	}
	if err := encodeKV(enc, "nsp", func() error { return enc.EncodeString(nsp) }); err != nil {
		return nil, err
	}
	if p.ID != nil {
		if err := encodeKV(enc, "id", func() error { return enc.EncodeUint(*p.ID) }); err != nil {
			return nil, err
		}
	}
	if err := w.allUsed(); err != nil {
		return nil, err
	}

	if err := enc.EncodeMapLen(3); err != nil {
		return nil, err
	}
	if err := encodeKV(enc, "rooms", func() error { return encodeStrings(enc, b.Options.Rooms) }); err != nil {
		return nil, err
	}
	if err := encodeKV(enc, "except", func() error { return encodeStrings(enc, b.Options.Except) }); err != nil {
		return nil, err
	}
	if err := encodeKV(enc, "flags", func() error { return encodeFlags(enc, b.Options.Flags) }); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encodeKV(enc *msgpack.Encoder, key string, value func() error) error {
	if err := enc.EncodeString(key); err != nil {
		return err
	}
	return value()
}

func encodeStrings(enc *msgpack.Encoder, ss []string) error {
	if err := enc.EncodeArrayLen(len(ss)); err != nil {
		return err
	}
	for _, s := range ss {
		if err := enc.EncodeString(s); err != nil {
			return err
		}
	}
	return nil
}

func encodeFlags(enc *msgpack.Encoder, f *Flags) error {
	if f == nil {
		f = &Flags{}
	}
	n := 0
	for _, set := range []bool{f.Volatile != nil, f.Compress != nil, f.Timeout != nil} {
		if set {
			n++
		}
	}
	if err := enc.EncodeMapLen(n); err != nil {
		return err
	}
	if f.Volatile != nil {
		if err := encodeKV(enc, "volatile", func() error { return enc.EncodeBool(*f.Volatile) }); err != nil {
			return err
		}
	}
	if f.Compress != nil {
		if err := encodeKV(enc, "compress", func() error { return enc.EncodeBool(*f.Compress) }); err != nil {
			return err
		}
	}
	if f.Timeout != nil {
		return encodeKV(enc, "timeout", func() error { return enc.EncodeInt(*f.Timeout) })
	}
	return nil
}
