package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sshaplygin/go-socket.io/adapter"
	"github.com/sshaplygin/go-socket.io/logger"
)

var redisRequestTimeout = 5 * time.Second

const (
	clientsRequest = iota
	clientRoomsRequest
	allRoomsRequest
	remoteJoinRequest
	remoteLeaveRequest
	customRequest
	remoteDisconnectRequest
)

type wireRequest struct {
	ID    string          `json:"requestid"`
	Type  *int            `json:"type"`
	Rooms []string        `json:"rooms,omitempty"`
	SID   string          `json:"sid,omitempty"`
	Room  *string         `json:"room,omitempty"`
	Close bool            `json:"close,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}
type wireResponse struct {
	ID        string          `json:"requestid"`
	Responder string          `json:"responder,omitempty"`
	Clients   []string        `json:"clients,omitempty"`
	Rooms     []string        `json:"rooms,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}
type clusterRequest struct {
	mu                       sync.Mutex
	kind, expected, received int
	done                     chan struct{}
	terminal                 bool
	err                      error
	values                   map[string]bool
	replies                  []json.RawMessage
	responders               map[string]bool
}

func (r *clusterRequest) finish(err error) { r.mu.Lock(); defer r.mu.Unlock(); r.finishLocked(err) }
func (r *clusterRequest) finishLocked(err error) {
	if !r.terminal {
		r.terminal = true
		r.err = err
		close(r.done)
	}
}
func (bc *broadcast) setRequest(id string, req interface{}) {
	bc.requestLock.Lock()
	defer bc.requestLock.Unlock()
	if req == nil {
		delete(bc.requests, id)
	} else {
		bc.requests[id] = req
	}
}

func (bc *broadcast) request(ctx context.Context, req wireRequest, seed []string) (*clusterRequest, error) {
	ctx = nonNilContext(ctx)
	bc.initCluster(nil)
	wait, cancel := context.WithTimeout(ctx, bc.requestTimeout)
	stopLife := context.AfterFunc(bc.clusterContext, cancel)
	defer func() { stopLife(); cancel() }()
	if bc.closed.Load() {
		return nil, ErrAdapterClosed
	}
	pending := &clusterRequest{kind: *req.Type, expected: 1, done: make(chan struct{}), values: map[string]bool{}, responders: map[string]bool{}}
	for _, v := range seed {
		pending.values[v] = true
	}
	if err := wait.Err(); err != nil {
		return pending, bc.classifyRequestError(ctx, wait, err)
	}
	if err := bc.waitReady(wait); err != nil {
		return pending, bc.classifyRequestError(ctx, wait, err)
	}
	expected := 1
	if *req.Type == clientsRequest || *req.Type == allRoomsRequest || *req.Type == customRequest {
		reply, err := bc.boundedDo(wait, "PUBSUB", "NUMSUB", bc.reqChannel)
		if err != nil {
			return pending, bc.classifyRequestError(ctx, wait, err)
		}
		expected, err = parseSubscriberCount(reply, bc.reqChannel)
		if err != nil {
			return pending, err
		}
		if expected < 1 {
			return pending, fmt.Errorf("redis: namespace subscription unavailable")
		}
	}
	pending.expected = expected
	req.ID = uuid.NewString()
	encoded, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	bc.requestLock.Lock()
	if bc.closed.Load() {
		bc.requestLock.Unlock()
		return nil, ErrAdapterClosed
	}
	bc.requests[req.ID] = pending
	bc.requestLock.Unlock()
	defer bc.setRequest(req.ID, nil)
	if _, err = bc.boundedDo(wait, "PUBLISH", bc.reqChannel, encoded); err != nil {
		pending.finish(bc.classifyRequestError(ctx, wait, err))
	}
	select {
	case <-pending.done:
	case <-wait.Done():
		pending.finish(bc.classifyRequestError(ctx, wait, wait.Err()))
	}
	pending.mu.Lock()
	defer pending.mu.Unlock()
	return pending, pending.err
}
func (bc *broadcast) classifyRequestError(caller, wait context.Context, err error) error {
	if bc.closed.Load() {
		return ErrAdapterClosed
	}
	return requestContextError(caller, wait, err)
}
func requestContextError(caller, wait context.Context, err error) error {
	if cause := requestDeadlineError(caller); cause != nil {
		return cause
	}
	if requestDeadlineError(wait) != nil {
		return ErrRequestTimeout
	}
	return err
}

// A socket deadline may fire before the context timer's goroutine runs.
// Check the deadline itself so that scheduling does not change the API error.
func requestDeadlineError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}
func nonNilContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
func requestOf(kind int) wireRequest { return wireRequest{Type: &kind} }
func requestValues(p *clusterRequest) []string {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, 0, len(p.values))
	for v := range p.values {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func (bc *broadcast) IDsContext(ctx context.Context, rooms ...string) ([]string, error) {
	req := requestOf(clientsRequest)
	req.Rooms = rooms
	p, err := bc.request(ctx, req, bc.Memory.IDs(rooms...))
	return requestValues(p), err
}
func (bc *broadcast) AllRoomsContext(ctx context.Context) ([]string, error) {
	p, err := bc.request(ctx, requestOf(allRoomsRequest), bc.Memory.AllRooms())
	return requestValues(p), err
}
func (bc *broadcast) LenContext(ctx context.Context, room string) (int, error) {
	ids, err := bc.IDsContext(ctx, room)
	return len(ids), err
}
func (bc *broadcast) ClientRooms(ctx context.Context, sid string) ([]string, error) {
	req := requestOf(clientRoomsRequest)
	req.SID = sid
	p, err := bc.request(ctx, req, nil)
	return requestValues(p), err
}
func (bc *broadcast) RemoteJoin(ctx context.Context, sid, room string) error {
	req := requestOf(remoteJoinRequest)
	req.SID = sid
	req.Room = &room
	_, err := bc.request(ctx, req, nil)
	return err
}
func (bc *broadcast) RemoteLeave(ctx context.Context, sid, room string) error {
	req := requestOf(remoteLeaveRequest)
	req.SID = sid
	req.Room = &room
	_, err := bc.request(ctx, req, nil)
	return err
}
func (bc *broadcast) RemoteDisconnect(ctx context.Context, sid string, closeEngine bool) error {
	req := requestOf(remoteDisconnectRequest)
	req.SID = sid
	req.Close = closeEngine
	_, err := bc.request(ctx, req, nil)
	return err
}
func (bc *broadcast) CustomRequest(ctx context.Context, data json.RawMessage) ([]json.RawMessage, error) {
	if len(data) == 0 {
		data = json.RawMessage("null")
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("redis: invalid custom request JSON")
	}
	req := requestOf(customRequest)
	req.Data = append(json.RawMessage(nil), data...)
	p, err := bc.request(ctx, req, nil)
	if p == nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]json.RawMessage(nil), p.replies...), err
}
func (bc *broadcast) ClearContext(ctx context.Context, room string) error {
	ctx = nonNilContext(ctx)
	bc.initCluster(nil)
	wait, cancel := context.WithTimeout(ctx, bc.requestTimeout)
	defer cancel()
	ids, err := bc.IDsContext(wait, room)
	if err != nil {
		return bc.classifyRequestError(ctx, wait, err)
	}
	for _, id := range ids {
		if err := bc.RemoteLeave(wait, id, room); err != nil {
			return bc.classifyRequestError(ctx, wait, err)
		}
	}
	return nil
}
func (bc *broadcast) IDs(rooms ...string) []string {
	ids, err := bc.IDsContext(context.Background(), rooms...)
	bc.logRequestError(err)
	return ids
}
func (bc *broadcast) AllRooms() []string {
	rooms, err := bc.AllRoomsContext(context.Background())
	bc.logRequestError(err)
	return rooms
}
func (bc *broadcast) Len(room string) int {
	n, err := bc.LenContext(context.Background(), room)
	if err != nil {
		bc.logRequestError(err)
		return -1
	}
	return n
}
func (bc *broadcast) logRequestError(err error) {
	if err != nil {
		logger.Log.Warn("socketio: redis cluster request failed", "nsp", bc.nsp, "err", err)
	}
}

func (bc *broadcast) onResponse(data []byte) {
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return
	}
	var id string
	if json.Unmarshal(raw["requestid"], &id) != nil || id == "" {
		return
	}
	bc.requestLock.Lock()
	p, _ := bc.requests[id].(*clusterRequest)
	bc.requestLock.Unlock()
	if p == nil {
		return
	}
	var values []string
	var reply json.RawMessage
	switch p.kind {
	case clientsRequest:
		if !decodeStringArray(raw["clients"], &values) {
			return
		}
	case allRoomsRequest, clientRoomsRequest:
		if !decodeStringArray(raw["rooms"], &values) {
			return
		}
	case customRequest:
		reply = raw["data"]
		if !json.Valid(reply) {
			return
		}
	}
	var responder string
	if field, ok := raw["responder"]; ok {
		if json.Unmarshal(field, &responder) != nil || responder == "" {
			return
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.terminal || (responder != "" && p.responders[responder]) {
		return
	}
	if responder != "" {
		p.responders[responder] = true
	}
	for _, v := range values {
		p.values[v] = true
	}
	if p.kind == customRequest {
		p.replies = append(p.replies, append(json.RawMessage(nil), reply...))
	}
	p.received++
	if p.received >= p.expected {
		p.finishLocked(nil)
	}
}
func decodeStringArray(raw json.RawMessage, out *[]string) bool {
	return len(raw) > 0 && raw[0] == '[' && json.Unmarshal(raw, out) == nil
}
func (bc *broadcast) onRequest(data []byte) { bc.handleClusterRequest([]*broadcast{bc}, data) }
func (bc *broadcast) handleClusterRequest(group []*broadcast, data []byte) {
	var req wireRequest
	if json.Unmarshal(data, &req) != nil || req.ID == "" || req.Type == nil || *req.Type < clientsRequest || *req.Type > remoteDisconnectRequest {
		return
	}
	response := wireResponse{ID: req.ID, Responder: bc.uid}
	switch *req.Type {
	case clientsRequest, allRoomsRequest:
		values := map[string]bool{}
		for _, member := range group {
			if member.closed.Load() {
				continue
			}
			var list []string
			if *req.Type == clientsRequest {
				list = member.Memory.IDs(req.Rooms...)
			} else {
				list = member.Memory.AllRooms()
			}
			for _, v := range list {
				values[v] = true
			}
		}
		out := make([]string, 0, len(values))
		for v := range values {
			out = append(out, v)
		}
		sort.Strings(out)
		// Empty arrays must remain arrays: Node rejects null or omitted query fields.
		payload := map[string]interface{}{"requestid": req.ID, "responder": bc.uid}
		if *req.Type == clientsRequest {
			payload["clients"] = out
		} else {
			payload["rooms"] = out
		}
		bc.replyCluster(payload)
	case customRequest:
		if len(req.Data) == 0 {
			req.Data = json.RawMessage("null")
		}
		bc.runClusterWork(func(ctx context.Context) {
			defer bc.recoverClusterWorker()
			bc.initCluster(nil)
			answer, err := bc.customHandler(ctx, append(json.RawMessage(nil), req.Data...))
			if err != nil {
				bc.logRequestError(err)
				return
			}
			if len(answer) == 0 {
				answer = json.RawMessage("null")
			}
			if !json.Valid(answer) {
				bc.logRequestError(fmt.Errorf("invalid custom response JSON"))
				return
			}
			response.Data = answer
			bc.replyCluster(response)
		})
	default:
		if req.SID == "" {
			return
		}
		bc.runClusterWork(func(ctx context.Context) {
			defer bc.recoverClusterWorker()
			for _, member := range group {
				if member.closed.Load() {
					continue
				}
				socket := member.lookupSocket(req.SID)
				if socket == nil {
					continue
				}
				if ctx.Err() != nil {
					return
				}
				switch *req.Type {
				case clientRoomsRequest:
					rooms := member.Memory.Rooms(socket)
					if rooms == nil {
						rooms = []string{}
					}
					bc.replyCluster(map[string]interface{}{"requestid": req.ID, "responder": bc.uid, "rooms": rooms})
					return
				case remoteJoinRequest:
					if req.Room == nil {
						return
					}
					target, ok := socket.(interface{ Join(...string) })
					if !ok {
						return
					}
					target.Join(*req.Room)
				case remoteLeaveRequest:
					if req.Room == nil {
						return
					}
					target, ok := socket.(interface{ Leave(string) })
					if !ok {
						return
					}
					target.Leave(*req.Room)
				case remoteDisconnectRequest:
					if req.Close {
						target, ok := socket.(interface{ Close() error })
						if !ok {
							return
						}
						if err := target.Close(); err != nil {
							bc.logRequestError(err)
							return
						}
					} else {
						target, ok := socket.(interface{ Disconnect() })
						if !ok {
							return
						}
						target.Disconnect()
					}
				}
				bc.replyCluster(response)
				return
			}
		})
	}
}
func (bc *broadcast) replyCluster(value interface{}) {
	bc.initCluster(nil)
	ctx, cancel := context.WithTimeout(bc.clusterContext, bc.requestTimeout)
	defer cancel()
	data, err := json.Marshal(value)
	if err == nil {
		_, err = bc.boundedDo(ctx, "PUBLISH", bc.resChannel, data)
	}
	bc.logRequestError(err)
}
func (bc *broadcast) recoverClusterWorker() {
	if recovered := recover(); recovered != nil {
		bc.logRequestError(fmt.Errorf("redis remote handler panic: %v", recovered))
	}
}

var _ adapter.Adapter = (*broadcast)(nil)
