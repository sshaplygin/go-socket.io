package redis

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

// redisRequestTimeout bounds how long Len and AllRooms wait for the answers
// of the subscribers counted by PUBSUB NUMSUB; an instance that died or hangs
// is still counted until Redis drops its connection.
var redisRequestTimeout = 5 * time.Second

// request types
const (
	roomLenReqType   = "0"
	clearRoomReqType = "1"
	allRoomReqType   = "2"
)

// request structs
type roomLenRequest struct {
	RequestType string
	RequestID   string
	Room        string
	numSub      int        `json:"-"`
	msgCount    int        `json:"-"`
	connections int        `json:"-"`
	mutex       sync.Mutex `json:"-"`
	done        chan bool  `json:"-"`
}

type clearRoomRequest struct {
	RequestType string
	RequestID   string
	Room        string
	UUID        string
}

type allRoomRequest struct {
	RequestType string
	RequestID   string
	rooms       map[string]bool `json:"-"`
	numSub      int             `json:"-"`
	msgCount    int             `json:"-"`
	mutex       sync.Mutex      `json:"-"`
	done        chan bool       `json:"-"`
}

// response struct
type roomLenResponse struct {
	RequestType string
	RequestID   string
	Connections int
}

type allRoomResponse struct {
	RequestType string
	RequestID   string
	Rooms       []string
}

// AllRooms gives list of all rooms available for broadcast.
// It waits at most redisRequestTimeout for the answers of the instances
// subscribed to the namespace and returns the union of the rooms received
// by then, or an empty list when publishing the request fails.
func (bc *broadcast) AllRooms() []string {
	req := allRoomRequest{
		RequestType: allRoomReqType,
		RequestID:   uuid.NewString(),
	}
	reqJSON, _ := json.Marshal(&req)

	req.rooms = make(map[string]bool)
	numSub, _ := bc.getNumSub(bc.reqChannel)
	req.numSub = numSub
	req.done = make(chan bool, 1)

	bc.setRequest(req.RequestID, &req)
	defer bc.setRequest(req.RequestID, nil)
	_, err := bc.do("PUBLISH", bc.reqChannel, reqJSON)
	if err != nil {
		return []string{} // if error occurred,return empty
	}

	waitAnswers(req.done)

	req.mutex.Lock()
	defer req.mutex.Unlock()
	rooms := make([]string, 0, len(req.rooms))
	for room := range req.rooms {
		rooms = append(rooms, room)
	}

	return rooms
}

// Len gives number of connections in the room.
// It waits at most redisRequestTimeout for the answers of the instances
// subscribed to the namespace and returns the sum of the answers received
// by then, or -1 when a Redis command fails.
func (bc *broadcast) Len(room string) int {
	req := roomLenRequest{
		RequestType: roomLenReqType,
		RequestID:   uuid.NewString(),
		Room:        room,
	}

	reqJSON, err := json.Marshal(&req)
	if err != nil {
		return -1
	}

	numSub, err := bc.getNumSub(bc.reqChannel)
	if err != nil {
		return -1
	}

	req.numSub = numSub

	req.done = make(chan bool, 1)

	bc.setRequest(req.RequestID, &req)
	defer bc.setRequest(req.RequestID, nil)
	_, err = bc.do("PUBLISH", bc.reqChannel, reqJSON)
	if err != nil {
		return -1
	}

	waitAnswers(req.done)

	req.mutex.Lock()
	defer req.mutex.Unlock()
	return req.connections
}

// setRequest registers a pending request, or removes it when req is nil.
func (bc *broadcast) setRequest(id string, req interface{}) {
	bc.requestLock.Lock()
	defer bc.requestLock.Unlock()
	if req == nil {
		delete(bc.requests, id)
	} else {
		bc.requests[id] = req
	}
}

// Get the number of subscribers of a channel.
func (bc *broadcast) getNumSub(channel string) (int, error) {
	rs, err := bc.do("PUBSUB", "NUMSUB", channel)
	if err != nil {
		return 0, err
	}

	numSub64, ok := rs.([]interface{})[1].(int64)
	if !ok {
		return 0, errors.New("redis reply cast to int error")
	}
	return int(numSub64), nil
}

// Handle request from redis channel.
func (bc *broadcast) onRequest(msg []byte) {
	var req map[string]string

	if err := json.Unmarshal(msg, &req); err != nil {
		return
	}

	var res interface{}
	switch req["RequestType"] {
	case roomLenReqType:
		res = roomLenResponse{
			RequestType: req["RequestType"],
			RequestID:   req["RequestID"],
			Connections: bc.Memory.Len(req["Room"]),
		}
		bc.publish(bc.resChannel, &res)

	case allRoomReqType:
		res := allRoomResponse{
			RequestType: req["RequestType"],
			RequestID:   req["RequestID"],
			Rooms:       bc.Memory.AllRooms(),
		}
		bc.publish(bc.resChannel, &res)

	case clearRoomReqType:
		if bc.uid == req["UUID"] {
			return
		}
		bc.Memory.Clear(req["Room"])

	default:
	}
}

// waitAnswers waits until all answers to a request arrived or
// redisRequestTimeout passed. On timeout the caller returns the answers
// gathered so far.
func waitAnswers(done <-chan bool) {
	timer := time.NewTimer(redisRequestTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
}

// notify signals done without blocking the dispatch goroutine when the
// request already completed or its caller stopped waiting.
func notify(done chan<- bool) {
	select {
	case done <- true:
	default:
	}
}

// Handle response from redis channel.
func (bc *broadcast) onResponse(msg []byte) {
	var res map[string]interface{}

	err := json.Unmarshal(msg, &res)
	if err != nil {
		return
	}

	id, _ := res["RequestID"].(string)
	bc.requestLock.Lock()
	req, ok := bc.requests[id]
	bc.requestLock.Unlock()
	if !ok {
		return
	}

	// Fields of a malformed answer count as empty.
	switch req := req.(type) {
	case *roomLenRequest:
		connections, _ := res["Connections"].(float64)

		req.mutex.Lock()
		req.msgCount++
		req.connections += int(connections)
		req.mutex.Unlock()

		if req.numSub == req.msgCount {
			notify(req.done)
		}

	case *allRoomRequest:
		rooms, _ := res["Rooms"].([]interface{})

		req.mutex.Lock()
		req.msgCount++
		for _, room := range rooms {
			if name, ok := room.(string); ok {
				req.rooms[name] = true
			}
		}
		req.mutex.Unlock()

		if req.numSub == req.msgCount {
			notify(req.done)
		}
	}
}

func (bc *broadcast) publishClear(room string) {
	req := clearRoomRequest{
		RequestType: clearRoomReqType,
		RequestID:   uuid.NewString(),
		Room:        room,
		UUID:        bc.uid,
	}

	bc.publish(bc.reqChannel, &req)
}
