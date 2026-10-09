package codec_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sshaplygin/go-socket.io/adapter/codec"
	"github.com/sshaplygin/go-socket.io/parser"
)

// unsupported lists the publications that are observations of the pinned adapter but
// not messages this package implements (see the package documentation).
var unsupported = map[string]bool{
	"broadcast-ack-request":   true, // MessagePack request, type 7
	"server-emit-response":    true, // JSON response with a type
	"broadcast-ack-responses": true, // type 8 JSON and type 9 MessagePack
}

// TestFixtureCorpus pins the corpus itself: 22 cases, unique names, and every
// recorded value equal to what a generic decoder makes of the recorded bytes.
func TestFixtureCorpus(t *testing.T) {
	f := loadFixtures(t)
	if len(f.Cases) != 22 {
		t.Fatalf("unexpected case count: %d", len(f.Cases))
	}
	seen := make(map[string]bool)
	for _, tc := range f.Cases {
		if tc.Name == "" || seen[tc.Name] {
			t.Fatalf("missing or duplicate case name %q", tc.Name)
		}
		seen[tc.Name] = true
		if tc.Name == "broadcast-local-no-publish" {
			if len(tc.Publications) != 0 {
				t.Fatal("a local broadcast must not publish")
			}
			continue
		}
		if len(tc.Publications) == 0 {
			t.Fatalf("%s: no publication", tc.Name)
		}
		for _, p := range tc.Publications {
			value, err := decodeGeneric(p.Encoding, p.wire(t))
			if err != nil {
				t.Fatalf("%s: %v", tc.Name, err)
			}
			got, err := json.Marshal(normalize(value))
			if err != nil {
				t.Fatal(err)
			}
			var actual, expected any
			if err := json.Unmarshal(got, &actual); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(p.Value, &expected); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, expected) {
				t.Errorf("%s: generic decode %s; recorded value %s", tc.Name, got, p.Value)
			}
		}
	}
}

// TestFixturesRoundtrip decodes every supported publication with the codec and checks
// that encoding the result gives the exact bytes Node published; every other
// publication must be rejected as unsupported.
func TestFixturesRoundtrip(t *testing.T) {
	f := loadFixtures(t)
	supported, rejected := 0, 0
	for _, tc := range f.Cases {
		for _, p := range tc.Publications {
			wire := p.wire(t)
			t.Run(tc.Name+"/"+p.Channel, func(t *testing.T) {
				var again []byte
				var err error
				switch p.kind() {
				case "broadcast":
					var b codec.Broadcast
					if b, err = codec.DecodeBroadcast(wire, codec.Limits{}); err == nil {
						again, err = codec.EncodeBroadcast(b)
					}
				case "request":
					var r codec.Request
					if r, err = codec.DecodeRequest(wire, codec.Limits{}); err == nil {
						again, err = codec.EncodeRequest(r)
					}
				case "response":
					var r codec.Response
					if r, err = codec.DecodeResponse(wire, codec.Limits{}); err == nil {
						again, err = codec.EncodeResponse(r)
					}
				}
				if unsupported[tc.Name] {
					rejected++
					if !errors.Is(err, codec.ErrUnsupported) {
						t.Fatalf("want ErrUnsupported, got %v", err)
					}
					return
				}
				supported++
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(again, wire) {
					t.Fatalf("re-encoded bytes differ\n got %q\nwant %q", again, wire)
				}
			})
		}
	}
	if supported != 18 || rejected != 4 {
		t.Fatalf("handled %d supported and %d unsupported publications, want 18 and 4", supported, rejected)
	}
}

func fixtureWire(t *testing.T, name string, index int) []byte {
	t.Helper()
	for _, tc := range loadFixtures(t).Cases {
		if tc.Name == name {
			return tc.Publications[index].wire(t)
		}
	}
	t.Fatalf("no fixture %q", name)
	return nil
}

func strs(s ...string) []string { return s }

func TestDecodeBroadcastFixtureValues(t *testing.T) {
	root, err := codec.DecodeBroadcast(fixtureWire(t, "broadcast-root", 0), codec.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if root.UID != "fixture-id" || root.Packet.Type != parser.Event || root.Packet.Namespace != "/" ||
		string(root.Packet.Data) != `["message","hello"]` || root.Packet.ID != nil || root.Packet.Attachments != nil {
		t.Errorf("root: %+v", root)
	}
	if len(root.Options.Rooms) != 0 || len(root.Options.Except) != 0 || root.Options.Flags == nil {
		t.Errorf("root options: %+v", root.Options)
	}

	room, err := codec.DecodeBroadcast(fixtureWire(t, "broadcast-room", 0), codec.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if room.Packet.Namespace != "/chat" || string(room.Packet.Data) != `["message",{"text":"€🙂"}]` ||
		!reflect.DeepEqual(room.Options.Rooms, strs("room-1")) {
		t.Errorf("room: %+v %s", room, room.Packet.Data)
	}

	union, err := codec.DecodeBroadcast(fixtureWire(t, "broadcast-union-except", 0), codec.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	fl := union.Options.Flags
	if !reflect.DeepEqual(union.Options.Rooms, strs("room-1", "room-2")) || !reflect.DeepEqual(union.Options.Except, strs("blocked")) ||
		fl == nil || fl.Volatile == nil || !*fl.Volatile || fl.Compress == nil || *fl.Compress || fl.Timeout != nil {
		t.Errorf("union: %+v", union.Options)
	}

	custom, err := codec.DecodeBroadcast(fixtureWire(t, "broadcast-custom-prefix", 0), codec.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if custom.Packet.Namespace != "/日本" || !reflect.DeepEqual(custom.Options.Rooms, strs("room#one")) {
		t.Errorf("custom: %+v", custom)
	}

	bin, err := codec.DecodeBroadcast(fixtureWire(t, "broadcast-binary", 0), codec.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if string(bin.Packet.Data) != `["image",{"file":{"_placeholder":true,"num":0}}]` ||
		!reflect.DeepEqual(bin.Packet.Attachments, [][]byte{{0x00, 0x04, 0xff}}) {
		t.Errorf("binary: %s %v", bin.Packet.Data, bin.Packet.Attachments)
	}

	empty, err := codec.DecodeBroadcast(fixtureWire(t, "broadcast-empty-binary", 0), codec.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if string(empty.Packet.Data) != `["image",{"_placeholder":true,"num":0}]` ||
		len(empty.Packet.Attachments) != 1 || empty.Packet.Attachments[0] == nil || len(empty.Packet.Attachments[0]) != 0 {
		t.Errorf("empty binary: %s %#v", empty.Packet.Data, empty.Packet.Attachments)
	}
}

func TestDecodeRequestFixtureValues(t *testing.T) {
	cases := []struct {
		name string
		want codec.Request
	}{
		{"join-request", codec.Request{UID: "fixture-id", Type: codec.RequestRemoteJoin,
			Options: &codec.Options{Rooms: strs("room-1"), Except: strs("blocked")}, Rooms: strs("new-room")}},
		{"leave-request", codec.Request{UID: "fixture-id", Type: codec.RequestRemoteLeave,
			Options: &codec.Options{Rooms: strs("room-1"), Except: []string{}}, Rooms: strs("old-room")}},
		{"disconnect-request", codec.Request{UID: "fixture-id", Type: codec.RequestRemoteDisconnect,
			Options: &codec.Options{Rooms: []string{}, Except: []string{}}, Close: true}},
		{"server-emit-request", codec.Request{UID: "fixture-id", Type: codec.RequestServerSideEmit,
			Data: json.RawMessage(`["notice",{"count":2}]`)}},
		{"server-emit-binary-json", codec.Request{UID: "fixture-id", Type: codec.RequestServerSideEmit,
			Data: json.RawMessage(`["notice",{"type":"Buffer","data":[0,255]}]`)}},
		{"server-emit-ack-request", codec.Request{UID: "fixture-id", RequestID: "fixture-id", Type: codec.RequestServerSideEmit,
			Data: json.RawMessage(`["notice"]`)}},
		{"all-rooms-request", codec.Request{UID: "fixture-id", RequestID: "fixture-id", Type: codec.RequestAllRooms}},
		{"fetch-sockets-request", codec.Request{UID: "fixture-id", RequestID: "fixture-id", Type: codec.RequestFetchSockets,
			Options: &codec.Options{Rooms: strs("room-1"), Except: strs("blocked")}}},
	}
	for _, c := range cases {
		got, err := codec.DecodeRequest(fixtureWire(t, c.name, 0), codec.Limits{})
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n got %+v\nwant %+v", c.name, got, c.want)
		}
	}
}

func TestDecodeResponseFixtureValues(t *testing.T) {
	ids, err := codec.DecodeResponse(fixtureWire(t, "sockets-response", 0), codec.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := ids.SocketIDs(); err != nil || ids.RequestID != "req" || !reflect.DeepEqual(got, strs("socket-1")) {
		t.Errorf("sockets-response: %+v %v %v", ids, got, err)
	}

	rooms, err := codec.DecodeResponse(fixtureWire(t, "all-rooms-response", 0), codec.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rooms.Rooms, strs("socket-1", "room-1")) || rooms.Sockets != nil {
		t.Errorf("all-rooms-response: %+v", rooms)
	}

	for _, name := range []string{"fetch-sockets-response", "specific-fetch-response"} {
		r, err := codec.DecodeResponse(fixtureWire(t, name, 0), codec.Limits{})
		if err != nil {
			t.Fatal(err)
		}
		socks, err := r.RemoteSockets()
		if err != nil || len(socks) != 1 {
			t.Fatalf("%s: %v %v", name, socks, err)
		}
		s := socks[0]
		// The codec passes the peer's handshake through, auth included: redaction
		// is the adapter's job (socketio.RedactHandshake).
		if s.ID != "socket-1" || !reflect.DeepEqual(s.Rooms, strs("socket-1", "room-1")) ||
			string(s.Handshake) != `{"headers":{"x-example":"fixture"},"auth":{"role":"reader"}}` ||
			string(s.Data) != `{"nickname":"alice"}` {
			t.Errorf("%s: %+v %s", name, s, s.Handshake)
		}
		if _, err := r.SocketIDs(); !errors.Is(err, codec.ErrMalformed) {
			t.Errorf("%s: SocketIDs on snapshots: %v", name, err)
		}
	}
}

// TestRoundtripExport writes the Go-encoded counterpart of every publication to the
// file named by CODEC_ROUNDTRIP_OUT (a temporary file by default) for the Node check
// in testdata/reference/verify-go.cjs. Supported publications are re-encoded by the
// codec; the reference-only ones by a generic encoder.
func TestRoundtripExport(t *testing.T) {
	out := os.Getenv("CODEC_ROUNDTRIP_OUT")
	if out == "" {
		out = filepath.Join(t.TempDir(), "roundtrip.json")
	}
	f := loadFixtures(t)
	for i := range f.Cases {
		for j := range f.Cases[i].Publications {
			p := &f.Cases[i].Publications[j]
			wire := p.wire(t)
			var again []byte
			var err error
			if unsupported[f.Cases[i].Name] {
				var value any
				if value, err = decodeGeneric(p.Encoding, wire); err == nil {
					again, err = encodeGeneric(p.Encoding, value)
				}
			} else {
				switch p.kind() {
				case "broadcast":
					var b codec.Broadcast
					if b, err = codec.DecodeBroadcast(wire, codec.Limits{}); err == nil {
						again, err = codec.EncodeBroadcast(b)
					}
				case "request":
					var r codec.Request
					if r, err = codec.DecodeRequest(wire, codec.Limits{}); err == nil {
						again, err = codec.EncodeRequest(r)
					}
				default:
					var r codec.Response
					if r, err = codec.DecodeResponse(wire, codec.Limits{}); err == nil {
						again, err = codec.EncodeResponse(r)
					}
				}
			}
			if err != nil {
				t.Fatalf("%s: %v", f.Cases[i].Name, err)
			}
			p.WireBase64 = base64.StdEncoding.EncodeToString(again)
		}
	}
	body, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, body, 0o644); err != nil {
		t.Fatal(err)
	}
}
