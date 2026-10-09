package codec

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// RequestType is the "type" of a request. The values and names are those of the
// Node adapter's RequestType enumeration.
type RequestType uint8

// The request types this package encodes and decodes (JSON requests). The Node
// enumeration also has SOCKETS (0), which the pinned adapter only answers and never
// sends, BROADCAST (7), BROADCAST_CLIENT_COUNT (8) and BROADCAST_ACK (9), the cluster
// broadcast with acknowledgements that the roadmap excludes. They decode to
// ErrUnsupported.
const (
	RequestAllRooms         RequestType = 1 // ALL_ROOMS
	RequestRemoteJoin       RequestType = 2 // REMOTE_JOIN
	RequestRemoteLeave      RequestType = 3 // REMOTE_LEAVE
	RequestRemoteDisconnect RequestType = 4 // REMOTE_DISCONNECT
	RequestFetchSockets     RequestType = 5 // REMOTE_FETCH
	RequestServerSideEmit   RequestType = 6 // SERVER_SIDE_EMIT
)

// Request is a message on a request channel. UID is the sender's server id and
// RequestID correlates the responses; which of the other fields are used depends on
// Type:
//
//	RequestAllRooms          RequestID
//	RequestRemoteJoin        Options, Rooms (the rooms to join)
//	RequestRemoteLeave       Options, Rooms (the rooms to leave)
//	RequestRemoteDisconnect  Options, Close
//	RequestFetchSockets      RequestID, Options
//	RequestServerSideEmit    Data, and RequestID when an answer is expected
//
// Options carries Rooms and Except only; its Flags are neither written nor read.
// Data is the JSON array of the emit arguments, event name first. A binary argument
// of a server-side emit is whatever JSON.stringify makes of a Buffer on the Node side
// ({"type":"Buffer","data":[...]}); the format does not preserve binary values there.
type Request struct {
	UID       string
	RequestID string
	Type      RequestType
	Options   *Options
	Rooms     []string
	Close     bool
	Data      json.RawMessage
}

type wireRequestOptions struct {
	Rooms  []string `json:"rooms"`
	Except []string `json:"except"`
}

// wireRequest has the field order of the Node adapter's JSON.stringify calls.
type wireRequest struct {
	UID       string              `json:"uid"`
	RequestID string              `json:"requestId,omitempty"`
	Type      *int                `json:"type"`
	Opts      *wireRequestOptions `json:"opts,omitempty"`
	Rooms     *[]string           `json:"rooms,omitempty"`
	Close     *bool               `json:"close,omitempty"`
	Data      json.RawMessage     `json:"data,omitempty"`
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// marshalJSON marshals v without HTML escaping, as JSON.stringify does not escape.
func marshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// checkRequest enforces the fields each request type needs; it is shared by the
// encoder and the decoder so that what one writes the other accepts.
func checkRequest(r *Request, bad error) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", bad, fmt.Sprintf(format, args...))
	}
	if r.Type < RequestAllRooms || r.Type > RequestServerSideEmit {
		return fmt.Errorf("%w: request type %d", ErrUnsupported, r.Type)
	}
	if r.UID == "" {
		return fail("request without uid")
	}
	switch r.Type {
	case RequestAllRooms:
		if r.RequestID == "" {
			return fail("all-rooms request without requestId")
		}
	case RequestRemoteJoin, RequestRemoteLeave:
		if r.Options == nil {
			return fail("request type %d without opts", r.Type)
		}
		if r.Rooms == nil {
			return fail("request type %d without rooms", r.Type)
		}
	case RequestRemoteDisconnect:
		if r.Options == nil {
			return fail("disconnect request without opts")
		}
	case RequestFetchSockets:
		if r.RequestID == "" || r.Options == nil {
			return fail("fetch-sockets request needs requestId and opts")
		}
	case RequestServerSideEmit:
		if d := bytes.TrimLeft(r.Data, " \t\r\n"); len(d) == 0 || d[0] != '[' {
			return fail("server-side emit without an array as data")
		}
	}
	return nil
}

// EncodeRequest encodes r as the JSON object the Node adapter publishes on a request
// channel, with its key order and without HTML escaping. It fails with ErrInvalid when
// the fields do not fit the type (see Request) and with ErrUnsupported for a type
// this package does not implement.
func EncodeRequest(r Request) ([]byte, error) {
	if err := checkRequest(&r, ErrInvalid); err != nil {
		return nil, err
	}
	t := int(r.Type)
	w := wireRequest{UID: r.UID, RequestID: r.RequestID, Type: &t}
	if r.Options != nil {
		w.Opts = &wireRequestOptions{Rooms: nonNil(r.Options.Rooms), Except: nonNil(r.Options.Except)}
	}
	switch r.Type {
	case RequestRemoteJoin, RequestRemoteLeave:
		rooms := nonNil(r.Rooms)
		w.Rooms = &rooms
	case RequestRemoteDisconnect:
		w.Close = &r.Close
	case RequestServerSideEmit:
		w.Data = r.Data
	}
	return marshalJSON(w)
}

// DecodeRequest decodes a message received on a request channel. Unknown fields are
// ignored. A message that does not start with '{' is a MessagePack request (the
// cluster broadcast with acknowledgements), which this package does not implement:
// the error matches ErrUnsupported, as it does for a request type outside the
// supported set.
func DecodeRequest(msg []byte, lim Limits) (Request, error) {
	lim, err := lim.resolve()
	if err != nil {
		return Request{}, err
	}
	if err := lim.checkSize(msg); err != nil {
		return Request{}, err
	}
	if len(msg) == 0 || msg[0] != '{' {
		return Request{}, fmt.Errorf("%w: request is not a JSON object", ErrUnsupported)
	}
	var w wireRequest
	if err := json.Unmarshal(msg, &w); err != nil {
		return Request{}, fmt.Errorf("%w: request: %v", ErrMalformed, err)
	}
	if w.Type == nil {
		return Request{}, fmt.Errorf("%w: request without type", ErrMalformed)
	}
	if *w.Type < 0 || *w.Type > 255 {
		return Request{}, fmt.Errorf("%w: request type %d", ErrUnsupported, *w.Type)
	}
	r := Request{UID: w.UID, RequestID: w.RequestID, Type: RequestType(*w.Type)}
	if w.Opts != nil {
		r.Options = &Options{Rooms: w.Opts.Rooms, Except: w.Opts.Except}
	}
	if w.Rooms != nil {
		r.Rooms = *w.Rooms
		if r.Rooms == nil {
			r.Rooms = []string{}
		}
	}
	if w.Close != nil {
		r.Close = *w.Close
	}
	if len(w.Data) > 0 && string(w.Data) != "null" {
		r.Data = w.Data
	}
	if err := checkRequest(&r, ErrMalformed); err != nil {
		return Request{}, err
	}
	return r, nil
}
