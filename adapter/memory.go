package adapter

import (
	"reflect"
	"sync"
)

// Memory maintains namespace rooms and emits to local sockets.
type Memory struct {
	rooms map[string]map[string]Socket
	sids  map[string]map[string]struct{}

	lock sync.RWMutex
}

// NewMemory creates an empty in-memory namespace adapter.
func NewMemory() *Memory {
	return &Memory{
		rooms: make(map[string]map[string]Socket),
		sids:  make(map[string]map[string]struct{}),
	}
}

// Join joins the given connection to the Memory room
func (bc *Memory) Join(room string, connection Socket) {
	bc.lock.Lock()
	defer bc.lock.Unlock()

	if _, ok := bc.rooms[room]; !ok {
		bc.rooms[room] = make(map[string]Socket)
	}

	id := connection.ID()
	bc.rooms[room][id] = connection
	if bc.sids[id] == nil {
		bc.sids[id] = make(map[string]struct{})
	}
	bc.sids[id][room] = struct{}{}
}

// Leave leaves the given connection from given room (if exist)
func (bc *Memory) Leave(room string, connection Socket) {
	bc.lock.Lock()
	defer bc.lock.Unlock()
	bc.leave(room, connection.ID())
}

// leave updates both indexes with the write lock held.
func (bc *Memory) leave(room, id string) {
	delete(bc.rooms[room], id)
	if len(bc.rooms[room]) == 0 {
		delete(bc.rooms, room)
	}
	delete(bc.sids[id], room)
	if len(bc.sids[id]) == 0 {
		delete(bc.sids, id)
	}
}

// LeaveAll leaves the given connection's rooms without scanning other sockets.
func (bc *Memory) LeaveAll(connection Socket) {
	bc.lock.Lock()
	defer bc.lock.Unlock()
	id := connection.ID()
	for room := range bc.sids[id] {
		bc.leave(room, id)
	}
}

// Clear removes a room from both indexes.
func (bc *Memory) Clear(room string) {
	bc.lock.Lock()
	defer bc.lock.Unlock()
	for id := range bc.rooms[room] {
		bc.leave(room, id)
	}
}

// Send emits to a room, registering a trailing callback on each socket.
func (bc *Memory) Send(room, event string, args ...interface{}) {
	_ = bc.SendTo(Options{Rooms: []string{room}}, event, args...)
}

// SendAll emits once per socket, registering a trailing callback on each socket.
func (bc *Memory) SendAll(event string, args ...interface{}) {
	_ = bc.SendTo(Options{}, event, args...)
}

func (bc *Memory) SendTo(opts Options, event string, args ...interface{}) error {
	bc.lock.RLock()
	targets := broadcastMembers(bc.rooms, opts)
	bc.lock.RUnlock()
	deliverBroadcast(targets, opts, event, args...)
	return nil
}

func (bc *Memory) IDs(rooms ...string) []string {
	bc.lock.RLock()
	defer bc.lock.RUnlock()
	if len(rooms) == 0 {
		ids := make([]string, 0, len(bc.sids))
		for id := range bc.sids {
			ids = append(ids, id)
		}
		return ids
	}
	return broadcastIDs(broadcastMembers(bc.rooms, Options{Rooms: rooms}))
}

// broadcastMembers takes a snapshot while the caller holds the room read lock.
func broadcastMembers(rooms map[string]map[string]Socket, opts Options) []Socket {
	if len(opts.Rooms) == 1 && len(opts.Except) == 0 {
		members := rooms[opts.Rooms[0]]
		result := make([]Socket, 0, len(members))
		for _, socket := range members {
			result = append(result, socket)
		}
		return result
	}
	seen := make(map[string]bool, len(opts.Except))
	for _, id := range opts.Except {
		seen[id] = true
	}
	var result []Socket
	add := func(members map[string]Socket) {
		for id, c := range members {
			if !seen[id] {
				seen[id] = true
				result = append(result, c)
			}
		}
	}
	if len(opts.Rooms) == 0 {
		for _, members := range rooms {
			add(members)
		}
	} else {
		for _, room := range opts.Rooms {
			add(rooms[room])
		}
	}
	return result
}

func broadcastIDs(targets []Socket) []string {
	ids := make([]string, 0, len(targets))
	for _, c := range targets {
		ids = append(ids, c.ID())
	}
	return ids
}

// preparedEmitter is optional; an opaque packet is shared only among the same
// concrete implementation so third-party packet representations cannot mix.
type preparedEmitter interface {
	PrepareEmit(Options, string, ...interface{}) (interface{}, error)
	EmitPrepared(Options, interface{})
}

func deliverBroadcast(targets []Socket, opts Options, event string, args ...interface{}) {
	type preparedResult struct {
		packet interface{}
		err    error
	}
	var prepared map[reflect.Type]preparedResult
	for _, c := range targets {
		if emitter, ok := c.(preparedEmitter); ok {
			typ := reflect.TypeOf(c)
			result, exists := prepared[typ]
			if !exists {
				result.packet, result.err = emitter.PrepareEmit(opts, event, args...)
				if prepared == nil {
					prepared = make(map[reflect.Type]preparedResult)
				}
				prepared[typ] = result
			}
			if result.err == nil {
				emitter.EmitPrepared(opts, result.packet)
				continue
			}
		}
		if opts.NoCompress || opts.Volatile {
			c.EmitWithOptions(opts, event, args...)
		} else {
			c.Emit(event, args...)
		}
	}
}

// ForEach visits a snapshot of the room after releasing its lock.
func (bc *Memory) ForEach(room string, f EachFunc) {
	for _, connection := range bc.members(room) {
		f(connection)
	}
}

// members returns the connections in room, so that callers emit without holding bc.lock.
func (bc *Memory) members(room string) []Socket {
	bc.lock.RLock()
	defer bc.lock.RUnlock()

	occupants := bc.rooms[room]
	conns := make([]Socket, 0, len(occupants))
	for _, connection := range occupants {
		conns = append(conns, connection)
	}

	return conns
}

// Len gives number of connections in the room
func (bc *Memory) Len(room string) int {
	bc.lock.RLock()
	defer bc.lock.RUnlock()

	return len(bc.rooms[room])
}

// Rooms gives the list of all the rooms available for Memory in case of
// no connection is given, in case of a connection is given, it gives
// list of all the rooms the connection is joined to
func (bc *Memory) Rooms(connection Socket) []string {
	if connection == nil {
		return bc.AllRooms()
	}

	bc.lock.RLock()
	defer bc.lock.RUnlock()

	rooms := make([]string, 0, len(bc.sids[connection.ID()]))
	for room := range bc.sids[connection.ID()] {
		rooms = append(rooms, room)
	}
	return rooms
}

// AllRooms gives list of all rooms available for Memory
func (bc *Memory) AllRooms() []string {
	bc.lock.RLock()
	defer bc.lock.RUnlock()

	rooms := make([]string, 0, len(bc.rooms))
	for room := range bc.rooms {
		rooms = append(rooms, room)
	}

	return rooms
}
