package codec_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/sshaplygin/go-socket.io/adapter/codec"
)

// seedFixtures adds every recorded publication of the given kind to the corpus, so
// the seeds run as ordinary tests.
func seedFixtures(f *testing.F, kind string) {
	for _, tc := range loadFixtures(f).Cases {
		for _, p := range tc.Publications {
			if p.kind() == kind {
				f.Add(p.wire(f))
			}
		}
	}
}

// known reports whether err is one of the package's classified errors.
func known(err error) bool {
	return errors.Is(err, codec.ErrMalformed) || errors.Is(err, codec.ErrUnsupported) || errors.Is(err, codec.ErrLimit)
}

// A decoder must not panic, must classify its errors, and must only accept what the
// encoder can write back; the second encoding must equal the first.
func FuzzDecodeBroadcast(f *testing.F) {
	seedFixtures(f, "broadcast")
	f.Add([]byte{0x93, 0xa1, 'u', 0x81, 0xc4, 0x01, 0x01, 0x80})
	f.Add([]byte{0x93, 0xdb, 0xff, 0xff, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, msg []byte) {
		lim := codec.Limits{MaxMessageBytes: 1 << 16, MaxDepth: 8, MaxAttachments: 4}
		b, err := codec.DecodeBroadcast(msg, lim)
		if err != nil {
			if !known(err) {
				t.Fatalf("unclassified error: %v", err)
			}
			return
		}
		enc, err := codec.EncodeBroadcast(b)
		if err != nil {
			t.Fatalf("decoded %+v but cannot encode it: %v", b, err)
		}
		b2, err := codec.DecodeBroadcast(enc, codec.Limits{MaxDepth: 12, MaxAttachments: 4})
		if err != nil {
			t.Fatalf("encoder output does not decode: %v", err)
		}
		enc2, err := codec.EncodeBroadcast(b2)
		if err != nil || !bytes.Equal(enc, enc2) {
			t.Fatalf("encoding is not stable: %v\n%x\n%x", err, enc, enc2)
		}
		if b2.UID != b.UID || b2.Packet.Type != b.Packet.Type || b2.Packet.Namespace != b.Packet.Namespace ||
			!reflect.DeepEqual(b2.Packet.Attachments, b.Packet.Attachments) || !reflect.DeepEqual(b2.Packet.ID, b.Packet.ID) {
			t.Fatalf("roundtrip changed the message\n%+v\n%+v", b, b2)
		}
	})
}

func FuzzDecodeRequest(f *testing.F) {
	seedFixtures(f, "request")
	f.Add([]byte(`{"uid":"u","type":2,"opts":{"rooms":[],"except":[]},"rooms":[]}`))
	f.Fuzz(func(t *testing.T, msg []byte) {
		r, err := codec.DecodeRequest(msg, codec.Limits{MaxMessageBytes: 1 << 16})
		if err != nil {
			if !known(err) {
				t.Fatalf("unclassified error: %v", err)
			}
			return
		}
		enc, err := codec.EncodeRequest(r)
		if err != nil {
			t.Fatalf("decoded %+v but cannot encode it: %v", r, err)
		}
		r2, err := codec.DecodeRequest(enc, codec.Limits{})
		if err != nil {
			t.Fatalf("encoder output does not decode: %v", err)
		}
		enc2, err := codec.EncodeRequest(r2)
		if err != nil || !bytes.Equal(enc, enc2) {
			t.Fatalf("encoding is not stable: %v\n%s\n%s", err, enc, enc2)
		}
		if r2.UID != r.UID || r2.RequestID != r.RequestID || r2.Type != r.Type || r2.Close != r.Close || !reflect.DeepEqual(r2.Rooms, r.Rooms) {
			t.Fatalf("roundtrip changed the request\n%+v\n%+v", r, r2)
		}
	})
}

func FuzzDecodeResponse(f *testing.F) {
	seedFixtures(f, "response")
	f.Add([]byte(`{"requestId":"r","sockets":[{"id":"s","rooms":[]}]}`))
	f.Fuzz(func(t *testing.T, msg []byte) {
		r, err := codec.DecodeResponse(msg, codec.Limits{MaxMessageBytes: 1 << 16})
		if err != nil {
			if !known(err) {
				t.Fatalf("unclassified error: %v", err)
			}
			return
		}
		enc, err := codec.EncodeResponse(r)
		if err != nil {
			t.Fatalf("decoded %+v but cannot encode it: %v", r, err)
		}
		r2, err := codec.DecodeResponse(enc, codec.Limits{})
		if err != nil {
			t.Fatalf("encoder output does not decode: %v", err)
		}
		enc2, err := codec.EncodeResponse(r2)
		if err != nil || !bytes.Equal(enc, enc2) {
			t.Fatalf("encoding is not stable: %v\n%s\n%s", err, enc, enc2)
		}
		// The entry readers must classify their errors too.
		if _, err := r.SocketIDs(); err != nil && !errors.Is(err, codec.ErrMalformed) {
			t.Fatalf("SocketIDs: %v", err)
		}
		socks, err := r.RemoteSockets()
		if err != nil {
			if !errors.Is(err, codec.ErrMalformed) {
				t.Fatalf("RemoteSockets: %v", err)
			}
			return
		}
		if _, err := codec.NewRemoteSocketsResponse(r.RequestID, socks); err != nil {
			t.Fatalf("decoded snapshots %+v that cannot be encoded: %v", socks, err)
		}
		for _, s := range socks {
			if (s.Handshake != nil && !json.Valid(s.Handshake)) || (s.Data != nil && !json.Valid(s.Data)) {
				t.Fatalf("invalid JSON in %+v", s)
			}
		}
	})
}
