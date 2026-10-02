package socketio

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gomodule/redigo/redis"
)

// redisBroadcast gives Join, Leave & BroadcastTO server API support to socket.io along with room management
// map of rooms where each room contains a map of connection id to connections in that room
type redisBroadcast struct {
	// pub serves PUBLISH and PUBSUB commands. A redigo connection allows only
	// one concurrent caller, and these commands come from user goroutines and
	// the dispatch goroutine, so each command takes its own pooled connection.
	pub *redis.Pool
	// sub is read by the dispatch goroutine and replaced by it after a
	// receive error; subLock orders the replacement with close.
	sub     *redis.PubSubConn
	subLock sync.Mutex
	done    chan struct{}
	// dial opens a connection to the server; pattern is the broadcast channel
	// pattern the subscriber listens on.
	dial    func() (redis.Conn, error)
	pattern string

	nsp        string
	uid        string
	key        string
	reqChannel string
	resChannel string

	requests    map[string]interface{}
	requestLock sync.Mutex

	rooms map[string]map[string]Conn

	lock sync.RWMutex
}

// redisRequestTimeout bounds how long Len and AllRooms wait for the answers
// of the subscribers counted by PUBSUB NUMSUB; an instance that died or hangs
// is still counted until Redis drops its connection.
var redisRequestTimeout = 5 * time.Second

// Backoff between attempts to reopen the subscriber connection after a
// receive error; tests shorten it.
var (
	redisReconnectMin = 100 * time.Millisecond
	redisReconnectMax = 5 * time.Second
)

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

func newRedisBroadcast(nsp string, opts *RedisAdapterOptions) (*redisBroadcast, error) {
	addr := opts.getAddr()
	var redisOpts []redis.DialOption
	if len(opts.Password) > 0 {
		redisOpts = append(redisOpts, redis.DialPassword(opts.Password))
	}
	if opts.DB > 0 {
		redisOpts = append(redisOpts, redis.DialDatabase(opts.DB))
	}

	dial := func() (redis.Conn, error) {
		return redis.Dial(opts.Network, addr, redisOpts...)
	}
	pub := &redis.Pool{MaxIdle: 4, Dial: dial}
	// Dial the first publishing connection now to report an unreachable server.
	first := pub.Get()
	err := first.Err()
	_ = first.Close()
	if err != nil {
		return nil, err
	}

	uid := newV4UUID()
	rbc := &redisBroadcast{
		rooms:      make(map[string]map[string]Conn),
		requests:   make(map[string]interface{}),
		done:       make(chan struct{}),
		pub:        pub,
		dial:       dial,
		pattern:    fmt.Sprintf("%s#%s#*", opts.Prefix, nsp),
		key:        fmt.Sprintf("%s#%s#%s", opts.Prefix, nsp, uid),
		reqChannel: fmt.Sprintf("%s-request#%s", opts.Prefix, nsp),
		resChannel: fmt.Sprintf("%s-response#%s", opts.Prefix, nsp),
		nsp:        nsp,
		uid:        uid,
	}

	if rbc.sub, err = rbc.subscribe(); err != nil {
		_ = pub.Close()
		return nil, err
	}

	go rbc.dispatch()

	return rbc, nil
}

// subscribe opens a subscriber connection to the broadcast pattern and the
// request and response channels, and closes it again if that fails.
func (bc *redisBroadcast) subscribe() (*redis.PubSubConn, error) {
	c, err := bc.dial()
	if err != nil {
		return nil, err
	}
	sub := &redis.PubSubConn{Conn: c}
	if err = sub.PSubscribe(bc.pattern); err == nil {
		err = sub.Subscribe(bc.reqChannel, bc.resChannel)
	}
	if err != nil {
		_ = sub.Close()
		return nil, err
	}
	return sub, nil
}

// AllRooms gives list of all rooms available for redisBroadcast.
func (bc *redisBroadcast) AllRooms() []string {
	req := allRoomRequest{
		RequestType: allRoomReqType,
		RequestID:   newV4UUID(),
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

// Join joins the given connection to the redisBroadcast room.
func (bc *redisBroadcast) Join(room string, connection Conn) {
	bc.lock.Lock()
	defer bc.lock.Unlock()

	if _, ok := bc.rooms[room]; !ok {
		bc.rooms[room] = make(map[string]Conn)
	}

	bc.rooms[room][connection.ID()] = connection
}

// Leave leaves the given connection from given room (if exist)
func (bc *redisBroadcast) Leave(room string, connection Conn) {
	bc.lock.Lock()
	defer bc.lock.Unlock()

	if connections, ok := bc.rooms[room]; ok {
		delete(connections, connection.ID())

		if len(connections) == 0 {
			delete(bc.rooms, room)
		}
	}
}

// LeaveAll leaves the given connection from all rooms.
func (bc *redisBroadcast) LeaveAll(connection Conn) {
	bc.lock.Lock()
	defer bc.lock.Unlock()

	for room, connections := range bc.rooms {
		delete(connections, connection.ID())

		if len(connections) == 0 {
			delete(bc.rooms, room)
		}
	}
}

// Clear clears the room.
func (bc *redisBroadcast) Clear(room string) {
	bc.lock.Lock()
	defer bc.lock.Unlock()

	delete(bc.rooms, room)
	go bc.publishClear(room)
}

// Send sends given event & args to all the connections in the specified room.
func (bc *redisBroadcast) Send(room, event string, args ...interface{}) {
	bc.send(room, event, args...)
	bc.publishMessage(room, event, args...)
}

// SendAll sends given event & args to all the connections to all the rooms.
func (bc *redisBroadcast) SendAll(event string, args ...interface{}) {
	bc.sendAll(event, args...)
	bc.publishMessage("", event, args...)
}

// ForEach sends data returned by DataFunc, if room does not exits sends nothing.
func (bc *redisBroadcast) ForEach(room string, f EachFunc) {
	for _, connection := range bc.members(room, false) {
		f(connection)
	}
}

// Len gives number of connections in the room.
func (bc *redisBroadcast) Len(room string) int {
	req := roomLenRequest{
		RequestType: roomLenReqType,
		RequestID:   newV4UUID(),
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

// Rooms gives the list of all the rooms available for redisBroadcast in case of
// no connection is given, in case of a connection is given, it gives
// list of all the rooms the connection is joined to.
func (bc *redisBroadcast) Rooms(connection Conn) []string {
	// AllRooms waits for this instance's own answer, which takes bc.lock.
	if connection == nil {
		return bc.AllRooms()
	}

	return bc.getRoomsByConn(connection)
}

func (bc *redisBroadcast) onMessage(channel string, msg []byte) error {
	channelParts := strings.Split(channel, "#")
	nsp := channelParts[len(channelParts)-2]
	if bc.nsp != nsp {
		return nil
	}

	uid := channelParts[len(channelParts)-1]
	if bc.uid == uid {
		return nil
	}

	var bcMessage map[string][]interface{}
	err := json.Unmarshal(msg, &bcMessage)
	if err != nil {
		return errors.New("invalid broadcast message")
	}

	args := bcMessage["args"]
	opts := bcMessage["opts"]
	if len(opts) < 2 {
		return errors.New("invalid broadcast message")
	}

	room, ok := opts[0].(string)
	if !ok {
		return errors.New("invalid room")
	}

	event, ok := opts[1].(string)
	if !ok {
		return errors.New("invalid event")
	}

	if room != "" {
		bc.send(room, event, args...)
	} else {
		bc.sendAll(event, args...)
	}

	return nil
}

// setRequest registers a pending request, or removes it when req is nil.
func (bc *redisBroadcast) setRequest(id string, req interface{}) {
	bc.requestLock.Lock()
	defer bc.requestLock.Unlock()
	if req == nil {
		delete(bc.requests, id)
	} else {
		bc.requests[id] = req
	}
}

// do runs one command on a pooled publishing connection.
func (bc *redisBroadcast) do(cmd string, args ...interface{}) (interface{}, error) {
	c := bc.pub.Get()
	defer func() { _ = c.Close() }()
	return c.Do(cmd, args...)
}

// Get the number of subscribers of a channel.
func (bc *redisBroadcast) getNumSub(channel string) (int, error) {
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
func (bc *redisBroadcast) onRequest(msg []byte) {
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
			Connections: bc.roomLen(req["Room"]),
		}
		bc.publish(bc.resChannel, &res)

	case allRoomReqType:
		res := allRoomResponse{
			RequestType: req["RequestType"],
			RequestID:   req["RequestID"],
			Rooms:       bc.allRooms(),
		}
		bc.publish(bc.resChannel, &res)

	case clearRoomReqType:
		if bc.uid == req["UUID"] {
			return
		}
		bc.clear(req["Room"])

	default:
	}
}

func (bc *redisBroadcast) publish(channel string, msg interface{}) {
	resJSON, err := json.Marshal(msg)
	if err != nil {
		return
	}

	_, err = bc.do("PUBLISH", channel, resJSON)
	if err != nil {
		return
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
func (bc *redisBroadcast) onResponse(msg []byte) {
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

func (bc *redisBroadcast) publishClear(room string) {
	req := clearRoomRequest{
		RequestType: clearRoomReqType,
		RequestID:   newV4UUID(),
		Room:        room,
		UUID:        bc.uid,
	}

	bc.publish(bc.reqChannel, &req)
}

func (bc *redisBroadcast) clear(room string) {
	bc.lock.Lock()
	defer bc.lock.Unlock()

	delete(bc.rooms, room)
}

func (bc *redisBroadcast) send(room string, event string, args ...interface{}) {
	for _, connection := range bc.members(room, false) {
		connection.Emit(event, args...)
	}
}

// members returns the connections in room, or with all set the connections
// of every room, once per room. Callers emit to them after bc.lock is
// released, so a connection can leave its rooms while it is emitted to.
func (bc *redisBroadcast) members(room string, all bool) []Conn {
	bc.lock.RLock()
	defer bc.lock.RUnlock()

	rooms := bc.rooms
	if !all {
		rooms = map[string]map[string]Conn{room: bc.rooms[room]}
	}

	var conns []Conn
	for _, connections := range rooms {
		for _, connection := range connections {
			conns = append(conns, connection)
		}
	}
	return conns
}

func (bc *redisBroadcast) publishMessage(room string, event string, args ...interface{}) {
	opts := make([]interface{}, 2)
	opts[0] = room
	opts[1] = event

	bcMessage := map[string][]interface{}{
		"opts": opts,
		"args": args,
	}
	bcMessageJSON, err := json.Marshal(bcMessage)
	if err != nil {
		return
	}

	_, err = bc.do("PUBLISH", bc.key, bcMessageJSON)
	if err != nil {
		return
	}
}

func (bc *redisBroadcast) sendAll(event string, args ...interface{}) {
	for _, connection := range bc.members("", true) {
		connection.Emit(event, args...)
	}
}

func (bc *redisBroadcast) allRooms() []string {
	bc.lock.RLock()
	defer bc.lock.RUnlock()

	rooms := make([]string, 0, len(bc.rooms))
	for room := range bc.rooms {
		rooms = append(rooms, room)
	}

	return rooms
}

func (bc *redisBroadcast) roomLen(room string) int {
	bc.lock.RLock()
	defer bc.lock.RUnlock()

	return len(bc.rooms[room])
}

func (bc *redisBroadcast) getRoomsByConn(connection Conn) []string {
	bc.lock.RLock()
	defer bc.lock.RUnlock()

	var rooms []string

	for room, connections := range bc.rooms {
		if _, ok := connections[connection.ID()]; ok {
			rooms = append(rooms, room)
		}
	}

	return rooms
}

// dispatch handles the messages of the subscriber connection until the
// broadcast is closed. Malformed messages are skipped; after a receive error
// the subscriber connection is reopened.
func (bc *redisBroadcast) dispatch() {
	sub := bc.sub
	delay := redisReconnectMin
	for {
		switch m := sub.Receive().(type) {
		case redis.Message:
			switch m.Channel {
			case bc.reqChannel:
				bc.onRequest(m.Data)
			case bc.resChannel:
				bc.onResponse(m.Data)
			default:
				_ = bc.onMessage(m.Channel, m.Data)
			}

		case redis.Subscription:
			if m.Count == 0 {
				return
			}
			// The server accepted the subscription, so the backoff starts over.
			delay = redisReconnectMin

		case error:
			_ = sub.Close()
			if sub, delay = bc.resubscribe(delay); sub == nil {
				return
			}
		}
	}
}

// resubscribe opens a new subscriber connection, waiting delay before the
// first attempt and doubling it up to redisReconnectMax after each attempt.
// It returns the connection and the delay for the next reconnect, so the
// backoff also grows when the server refuses each new subscription, or a
// nil connection once the broadcast is closed.
func (bc *redisBroadcast) resubscribe(delay time.Duration) (*redis.PubSubConn, time.Duration) {
	for ; ; delay = min(2*delay, redisReconnectMax) {
		select {
		case <-bc.done:
			return nil, 0
		case <-time.After(delay):
		}

		sub, err := bc.subscribe()
		if err != nil {
			continue
		}

		bc.subLock.Lock()
		defer bc.subLock.Unlock()
		select {
		case <-bc.done:
			_ = sub.Close()
			return nil, 0
		default:
			bc.sub = sub
			return sub, min(2*delay, redisReconnectMax)
		}
	}
}

// close stops the dispatch goroutine and closes the connections.
func (bc *redisBroadcast) close() {
	bc.subLock.Lock()
	defer bc.subLock.Unlock()
	select {
	case <-bc.done:
		return
	default:
	}

	close(bc.done)
	_ = bc.sub.Close()
	_ = bc.pub.Close()
}
