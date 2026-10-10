package codec

import (
	"encoding/json"
	"fmt"
)

// Response is a message on a response channel: the JSON object
// {"requestId", "sockets"|"rooms"} that answers an all-rooms or fetch-sockets request,
// or {"requestId"} alone. Rooms is nil when the key is absent. Sockets is nil when the
// key is absent and holds the raw entries otherwise, because the entry shape depends
// on the request that is being answered: SocketIDs for socket ids, RemoteSockets for
// snapshots. The Node server-side emit and broadcast acknowledgement responses carry
// a "type" key and decode to ErrUnsupported.
type Response struct {
	RequestID string
	Rooms     []string
	Sockets   []json.RawMessage
}

type wireResponse struct {
	RequestID string             `json:"requestId"`
	Type      *int               `json:"type,omitempty"`
	Sockets   *[]json.RawMessage `json:"sockets,omitempty"`
	Rooms     *[]string          `json:"rooms,omitempty"`
}

type wireSocket struct {
	ID        string          `json:"id"`
	Handshake json.RawMessage `json:"handshake,omitempty"`
	Rooms     []string        `json:"rooms"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// NewRoomsResponse returns the answer to an all-rooms request.
func NewRoomsResponse(requestID string, rooms []string) Response {
	return Response{RequestID: requestID, Rooms: nonNil(rooms)}
}

// NewSocketIDsResponse returns a "sockets" answer that lists socket ids.
func NewSocketIDsResponse(requestID string, ids []string) Response {
	raw := make([]json.RawMessage, len(ids))
	for i, id := range ids {
		raw[i] = appendJSONString(nil, id)
	}
	return Response{RequestID: requestID, Sockets: raw}
}

// NewRemoteSocketsResponse returns the answer to a fetch-sockets request. Snapshots
// are written as they are: redacting the handshake of a local socket is the caller's
// business.
func NewRemoteSocketsResponse(requestID string, sockets []RemoteSocket) (Response, error) {
	raw := make([]json.RawMessage, len(sockets))
	for i, s := range sockets {
		if s.ID == "" {
			return Response{}, fmt.Errorf("%w: socket %d without id", ErrInvalid, i)
		}
		if len(s.Handshake) > 0 && s.Handshake[0] != '{' {
			return Response{}, fmt.Errorf("%w: handshake of %q is not a JSON object", ErrInvalid, s.ID)
		}
		b, err := marshalJSON(wireSocket{ID: s.ID, Handshake: s.Handshake, Rooms: nonNil(s.Rooms), Data: s.Data})
		if err != nil {
			return Response{}, fmt.Errorf("socket %q: %w", s.ID, err)
		}
		raw[i] = b
	}
	return Response{RequestID: requestID, Sockets: raw}, nil
}

// EncodeResponse encodes r with the key order of the Node adapter. It fails with
// ErrInvalid for an empty RequestID, for Rooms and Sockets both set, and for entries
// that are not valid JSON.
func EncodeResponse(r Response) ([]byte, error) {
	if r.RequestID == "" {
		return nil, fmt.Errorf("%w: response without requestId", ErrInvalid)
	}
	if r.Rooms != nil && r.Sockets != nil {
		return nil, fmt.Errorf("%w: response with both rooms and sockets", ErrInvalid)
	}
	w := wireResponse{RequestID: r.RequestID}
	if r.Rooms != nil {
		w.Rooms = &r.Rooms
	}
	if r.Sockets != nil {
		w.Sockets = &r.Sockets
	}
	return marshalJSON(w)
}

// DecodeResponse decodes a message received on a response channel. Unknown fields are
// ignored. A message that does not start with '{' (a MessagePack broadcast
// acknowledgement) or that has a "type" key (server-side emit and broadcast
// acknowledgement responses) matches ErrUnsupported. A message with both a "rooms" and
// a "sockets" key is ErrMalformed, because EncodeResponse refuses such a Response.
func DecodeResponse(msg []byte, lim Limits) (Response, error) {
	lim, err := lim.resolve()
	if err != nil {
		return Response{}, err
	}
	if err := lim.checkSize(msg); err != nil {
		return Response{}, err
	}
	if len(msg) == 0 || msg[0] != '{' {
		return Response{}, fmt.Errorf("%w: response is not a JSON object", ErrUnsupported)
	}
	var w wireResponse
	if err := json.Unmarshal(msg, &w); err != nil {
		return Response{}, fmt.Errorf("%w: response: %v", ErrMalformed, err)
	}
	if w.Type != nil {
		return Response{}, fmt.Errorf("%w: response of type %d", ErrUnsupported, *w.Type)
	}
	if w.RequestID == "" {
		return Response{}, fmt.Errorf("%w: response without requestId", ErrMalformed)
	}
	if w.Rooms != nil && w.Sockets != nil {
		return Response{}, fmt.Errorf("%w: response with both rooms and sockets", ErrMalformed)
	}
	r := Response{RequestID: w.RequestID}
	if w.Rooms != nil {
		r.Rooms = nonNil(*w.Rooms)
	}
	if w.Sockets != nil {
		r.Sockets = *w.Sockets
		if r.Sockets == nil {
			r.Sockets = []json.RawMessage{}
		}
	}
	return r, nil
}

// SocketIDs reads Sockets as an array of socket ids, the shape of a "sockets" answer
// to a legacy sockets query. It returns nil, nil when Sockets is absent.
func (r Response) SocketIDs() ([]string, error) {
	if r.Sockets == nil {
		return nil, nil
	}
	ids := make([]string, len(r.Sockets))
	for i, raw := range r.Sockets {
		if err := json.Unmarshal(raw, &ids[i]); err != nil {
			return nil, fmt.Errorf("%w: socket id %d: %v", ErrMalformed, i, err)
		}
	}
	return ids, nil
}

// RemoteSockets reads Sockets as snapshots, the shape of the answer to a
// fetch-sockets request. It returns nil, nil when Sockets is absent. The snapshots
// are exactly what the peer sent: apply socketio.RedactHandshake to each Handshake.
func (r Response) RemoteSockets() ([]RemoteSocket, error) {
	if r.Sockets == nil {
		return nil, nil
	}
	out := make([]RemoteSocket, len(r.Sockets))
	for i, raw := range r.Sockets {
		var w wireSocket
		if err := json.Unmarshal(raw, &w); err != nil {
			return nil, fmt.Errorf("%w: socket %d: %v", ErrMalformed, i, err)
		}
		if w.ID == "" {
			return nil, fmt.Errorf("%w: socket %d without id", ErrMalformed, i)
		}
		s := RemoteSocket{ID: w.ID, Rooms: nonNil(w.Rooms)}
		if len(w.Handshake) > 0 && string(w.Handshake) != "null" {
			if w.Handshake[0] != '{' {
				return nil, fmt.Errorf("%w: handshake of %q is not an object", ErrMalformed, w.ID)
			}
			s.Handshake = w.Handshake
		}
		if len(w.Data) > 0 && string(w.Data) != "null" {
			s.Data = w.Data
		}
		out[i] = s
	}
	return out, nil
}
