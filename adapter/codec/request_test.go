package codec_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/sshaplygin/go-socket.io/adapter/codec"
)

func TestRequestRoundtrip(t *testing.T) {
	opts := &codec.Options{Rooms: strs("a"), Except: strs("b")}
	empty := &codec.Options{Rooms: []string{}, Except: []string{}}
	cases := map[string]codec.Request{
		"all rooms":     {UID: "u", RequestID: "r", Type: codec.RequestAllRooms},
		"join":          {UID: "u", Type: codec.RequestRemoteJoin, Options: opts, Rooms: strs("x")},
		"join no rooms": {UID: "u", Type: codec.RequestRemoteJoin, Options: empty, Rooms: []string{}},
		"leave":         {UID: "u", Type: codec.RequestRemoteLeave, Options: opts, Rooms: strs("x")},
		"disconnect":    {UID: "u", Type: codec.RequestRemoteDisconnect, Options: opts, Close: true},
		"disconnect 2":  {UID: "u", Type: codec.RequestRemoteDisconnect, Options: empty},
		"fetch":         {UID: "u", RequestID: "r", Type: codec.RequestFetchSockets, Options: opts},
		"emit":          {UID: "u", Type: codec.RequestServerSideEmit, Data: json.RawMessage(`["n",{"a":"<&>"}]`)},
		"emit ack":      {UID: "u", RequestID: "r", Type: codec.RequestServerSideEmit, Data: json.RawMessage(`["n"]`)},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			wire, err := codec.EncodeRequest(in)
			if err != nil {
				t.Fatal(err)
			}
			out, err := codec.DecodeRequest(wire, codec.Limits{})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(in, out) {
				t.Errorf("\n in %+v\nout %+v\nwire %s", in, out, wire)
			}
		})
	}
}

func TestEncodeRequestWire(t *testing.T) {
	// Key order, empty arrays instead of null, and no HTML escaping, as JSON.stringify.
	got, err := codec.EncodeRequest(codec.Request{UID: "u", Type: codec.RequestRemoteJoin, Options: &codec.Options{}, Rooms: nil})
	if !errors.Is(err, codec.ErrInvalid) {
		t.Errorf("join without rooms: %s %v", got, err)
	}
	got, err = codec.EncodeRequest(codec.Request{UID: "u", Type: codec.RequestRemoteJoin, Options: &codec.Options{}, Rooms: []string{}})
	if err != nil || string(got) != `{"uid":"u","type":2,"opts":{"rooms":[],"except":[]},"rooms":[]}` {
		t.Errorf("%s %v", got, err)
	}
	got, err = codec.EncodeRequest(codec.Request{UID: "u", Type: codec.RequestServerSideEmit, Data: json.RawMessage(` [ "<&>" ] `)})
	if err != nil || string(got) != `{"uid":"u","type":6,"data":["<&>"]}` {
		t.Errorf("%s %v", got, err)
	}
	// JSON.stringify writes U+2028 raw and encoding/json escapes it from Go 1.27 on:
	// the bytes differ, the JSON value is the same, and only the value is pinned.
	got, err = codec.EncodeRequest(codec.Request{UID: "u", Type: codec.RequestServerSideEmit, Data: json.RawMessage(`["\u2028"]`)})
	if err != nil {
		t.Fatal(err)
	}
	back, err := codec.DecodeRequest(got, codec.Limits{})
	var args []string
	if err != nil || json.Unmarshal(back.Data, &args) != nil || len(args) != 1 || args[0] != "\u2028" {
		t.Errorf("%s %v %v", got, back.Data, err)
	}
	// Options.Flags never reach a request.
	yes := true
	got, err = codec.EncodeRequest(codec.Request{UID: "u", RequestID: "r", Type: codec.RequestFetchSockets, Options: &codec.Options{Flags: &codec.Flags{Volatile: &yes}}})
	if err != nil || strings.Contains(string(got), "flags") {
		t.Errorf("%s %v", got, err)
	}
}

func TestRequestRejects(t *testing.T) {
	o := &codec.Options{}
	enc := map[string]struct {
		r    codec.Request
		want error
	}{
		"no uid":          {codec.Request{Type: codec.RequestAllRooms, RequestID: "r"}, codec.ErrInvalid},
		"all rooms no id": {codec.Request{UID: "u", Type: codec.RequestAllRooms}, codec.ErrInvalid},
		"join no opts":    {codec.Request{UID: "u", Type: codec.RequestRemoteJoin, Rooms: []string{}}, codec.ErrInvalid},
		"leave no rooms":  {codec.Request{UID: "u", Type: codec.RequestRemoteLeave, Options: o}, codec.ErrInvalid},
		"disconnect":      {codec.Request{UID: "u", Type: codec.RequestRemoteDisconnect}, codec.ErrInvalid},
		"fetch no id":     {codec.Request{UID: "u", Type: codec.RequestFetchSockets, Options: o}, codec.ErrInvalid},
		"fetch no opts":   {codec.Request{UID: "u", RequestID: "r", Type: codec.RequestFetchSockets}, codec.ErrInvalid},
		"emit no data":    {codec.Request{UID: "u", Type: codec.RequestServerSideEmit}, codec.ErrInvalid},
		"emit object":     {codec.Request{UID: "u", Type: codec.RequestServerSideEmit, Data: json.RawMessage(`{}`)}, codec.ErrInvalid},
		"emit bad JSON":   {codec.Request{UID: "u", Type: codec.RequestServerSideEmit, Data: json.RawMessage(`[`)}, codec.ErrInvalid},
		"sockets type 0":  {codec.Request{UID: "u", RequestID: "r"}, codec.ErrUnsupported},
		"broadcast 7":     {codec.Request{UID: "u", Type: 7}, codec.ErrUnsupported},
	}
	for name, c := range enc {
		if _, err := codec.EncodeRequest(c.r); !errors.Is(err, c.want) {
			t.Errorf("encode %s: %v", name, err)
		}
	}
	dec := map[string]struct {
		msg  string
		want error
	}{
		"empty":            {``, codec.ErrUnsupported},
		"msgpack":          {"\x83\xa3uid", codec.ErrUnsupported},
		"not an object":    {`[1]`, codec.ErrUnsupported},
		"truncated":        {`{"uid":"u"`, codec.ErrMalformed},
		"no type":          {`{"uid":"u"}`, codec.ErrMalformed},
		"type string":      {`{"uid":"u","type":"1"}`, codec.ErrMalformed},
		"type 0":           {`{"uid":"u","requestId":"r","type":0,"rooms":[]}`, codec.ErrUnsupported},
		"type 7":           {`{"uid":"u","type":7}`, codec.ErrUnsupported},
		"type 300":         {`{"uid":"u","type":300}`, codec.ErrUnsupported},
		"type -1":          {`{"uid":"u","type":-1}`, codec.ErrUnsupported},
		"no uid":           {`{"type":1,"requestId":"r"}`, codec.ErrMalformed},
		"all rooms no id":  {`{"uid":"u","type":1}`, codec.ErrMalformed},
		"join legacy form": {`{"uid":"u","requestId":"r","type":2,"sid":"s","room":"x"}`, codec.ErrMalformed},
		"join null rooms":  {`{"uid":"u","type":2,"opts":{"rooms":[],"except":[]},"rooms":null}`, codec.ErrMalformed},
		"rooms not array":  {`{"uid":"u","type":2,"opts":{"rooms":"a","except":[]},"rooms":[]}`, codec.ErrMalformed},
		"emit null":        {`{"uid":"u","type":6,"data":null}`, codec.ErrMalformed},
		"emit object":      {`{"uid":"u","type":6,"data":{}}`, codec.ErrMalformed},
	}
	for name, c := range dec {
		if _, err := codec.DecodeRequest([]byte(c.msg), codec.Limits{}); !errors.Is(err, c.want) {
			t.Errorf("decode %s: want %v, got %v", name, c.want, err)
		}
	}
	if _, err := codec.DecodeRequest([]byte(`{"uid":"u","type":1,"requestId":"r"}`), codec.Limits{MaxMessageBytes: 10}); !errors.Is(err, codec.ErrLimit) {
		t.Errorf("size limit: %v", err)
	}
}

func TestDecodeRequestIgnoresUnknownFields(t *testing.T) {
	r, err := codec.DecodeRequest([]byte(`{"uid":"u","requestId":"r","type":1,"future":{"a":[1]},"sid":"s"}`), codec.Limits{})
	if err != nil || r.Type != codec.RequestAllRooms || r.RequestID != "r" {
		t.Errorf("%+v %v", r, err)
	}
}

func TestResponseRoundtrip(t *testing.T) {
	socks := []codec.RemoteSocket{
		{ID: "s1", Rooms: strs("s1", "r"), Handshake: json.RawMessage(`{"headers":{"a":"b"},"auth":{"t":1}}`), Data: json.RawMessage(`{"n":1}`)},
		{ID: "s2"},
	}
	fetch, err := codec.NewRemoteSocketsResponse("r", socks)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := codec.EncodeResponse(fetch)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"requestId":"r","sockets":[{"id":"s1","handshake":{"headers":{"a":"b"},"auth":{"t":1}},"rooms":["s1","r"],"data":{"n":1}},{"id":"s2","rooms":[]}]}`
	if string(wire) != want {
		t.Errorf("got  %s\nwant %s", wire, want)
	}
	dec, err := codec.DecodeResponse(wire, codec.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := dec.RemoteSockets()
	if err != nil {
		t.Fatal(err)
	}
	socks[1].Rooms = []string{}
	if !reflect.DeepEqual(got, socks) {
		t.Errorf("\n got %+v\nwant %+v", got, socks)
	}

	for name, r := range map[string]codec.Response{
		"ids":        codec.NewSocketIDsResponse("r", strs("a", `b"c`)),
		"no ids":     codec.NewSocketIDsResponse("r", nil),
		"rooms":      codec.NewRoomsResponse("r", strs("a")),
		"no rooms":   codec.NewRoomsResponse("r", nil),
		"id only":    {RequestID: "r"},
		"no sockets": {RequestID: "r", Sockets: []json.RawMessage{}},
	} {
		wire, err := codec.EncodeResponse(r)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		back, err := codec.DecodeResponse(wire, codec.Limits{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if name == "no ids" {
			r.Sockets = []json.RawMessage{}
		}
		if !reflect.DeepEqual(r, back) {
			t.Errorf("%s: %s\n in %+v\nout %+v", name, wire, r, back)
		}
	}
	ids, err := codec.NewSocketIDsResponse("r", strs("a", `b"c`)).SocketIDs()
	if err != nil || !reflect.DeepEqual(ids, strs("a", `b"c`)) {
		t.Errorf("%v %v", ids, err)
	}
}

func TestSnapshotsPassTheHandshakeThrough(t *testing.T) {
	// The codec never redacts: a peer's auth and credential headers survive decoding, and
	// the adapter must apply socketio.RedactHandshake to each Handshake.
	msg := `{"requestId":"r","sockets":[{"id":"s","handshake":{"auth":{"token":"t"},"headers":{"Cookie":"c","authorization":"a","x-api-key":"k"},"query":{"token":"q"}},"rooms":["s"]}]}`
	r, err := codec.DecodeResponse([]byte(msg), codec.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	socks, err := r.RemoteSockets()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"auth":{"token":"t"}`, `"Cookie":"c"`, `"authorization":"a"`, `"x-api-key":"k"`, `"query":{"token":"q"}`} {
		if !strings.Contains(string(socks[0].Handshake), want) {
			t.Errorf("handshake lost %s: %s", want, socks[0].Handshake)
		}
	}
}

func TestResponseRejects(t *testing.T) {
	enc := map[string]codec.Response{
		"no id":         {Rooms: strs("a")},
		"both":          {RequestID: "r", Rooms: strs("a"), Sockets: []json.RawMessage{}},
		"invalid entry": {RequestID: "r", Sockets: []json.RawMessage{json.RawMessage(`{`)}},
	}
	for name, r := range enc {
		if _, err := codec.EncodeResponse(r); !errors.Is(err, codec.ErrInvalid) {
			t.Errorf("encode %s: %v", name, err)
		}
	}
	if _, err := codec.NewRemoteSocketsResponse("r", []codec.RemoteSocket{{}}); !errors.Is(err, codec.ErrInvalid) {
		t.Errorf("socket without id: %v", err)
	}
	if _, err := codec.NewRemoteSocketsResponse("r", []codec.RemoteSocket{{ID: "s", Handshake: json.RawMessage(`[]`)}}); !errors.Is(err, codec.ErrInvalid) {
		t.Errorf("handshake array: %v", err)
	}
	if _, err := codec.NewRemoteSocketsResponse("r", []codec.RemoteSocket{{ID: "s", Data: json.RawMessage(`{`)}}); !errors.Is(err, codec.ErrInvalid) {
		t.Errorf("invalid data: %v", err)
	}

	dec := map[string]struct {
		msg  string
		want error
	}{
		"empty":         {``, codec.ErrUnsupported},
		"msgpack":       {"\x83\xa4type", codec.ErrUnsupported},
		"typed":         {`{"type":6,"requestId":"r","data":1}`, codec.ErrUnsupported},
		"typed zero":    {`{"type":0,"requestId":"r"}`, codec.ErrUnsupported},
		"truncated":     {`{"requestId":"r"`, codec.ErrMalformed},
		"no id":         {`{"sockets":[]}`, codec.ErrMalformed},
		"id not text":   {`{"requestId":1}`, codec.ErrMalformed},
		"rooms wrong":   {`{"requestId":"r","rooms":{}}`, codec.ErrMalformed},
		"sockets wrong": {`{"requestId":"r","sockets":"s"}`, codec.ErrMalformed},
	}
	for name, c := range dec {
		if _, err := codec.DecodeResponse([]byte(c.msg), codec.Limits{}); !errors.Is(err, c.want) {
			t.Errorf("decode %s: want %v, got %v", name, c.want, err)
		}
	}

	bad := map[string]string{
		"entry not an object": `{"requestId":"r","sockets":["s"]}`,
		"no id":               `{"requestId":"r","sockets":[{"rooms":[]}]}`,
		"handshake array":     `{"requestId":"r","sockets":[{"id":"s","handshake":[]}]}`,
		"rooms wrong":         `{"requestId":"r","sockets":[{"id":"s","rooms":"x"}]}`,
	}
	for name, msg := range bad {
		r, err := codec.DecodeResponse([]byte(msg), codec.Limits{})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := r.RemoteSockets(); !errors.Is(err, codec.ErrMalformed) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// null handshake and data mean none.
	r, err := codec.DecodeResponse([]byte(`{"requestId":"r","sockets":[{"id":"s","handshake":null,"rooms":null,"data":null}]}`), codec.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	socks, err := r.RemoteSockets()
	if err != nil || len(socks) != 1 || socks[0].Handshake != nil || socks[0].Data != nil || socks[0].Rooms == nil {
		t.Errorf("%+v %v", socks, err)
	}
	if _, err := (codec.Response{RequestID: "r"}).SocketIDs(); err != nil {
		t.Error(err)
	}
	if _, err := codec.DecodeResponse([]byte(`{"requestId":"r"}`), codec.Limits{MaxMessageBytes: 5}); !errors.Is(err, codec.ErrLimit) {
		t.Errorf("size limit: %v", err)
	}
}
