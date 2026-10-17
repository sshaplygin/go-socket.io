package socketio

import "sync"

// EachFunc typed for each callback function
type EachFunc func(Conn)

// BroadcastOptions selects the namespace sockets and flags of one broadcast.
// The zero value targets every socket of the namespace.
type BroadcastOptions struct {
	// Rooms selects a union of rooms, once per socket. Empty means every socket.
	Rooms []string
	// Except lists socket IDs that receive nothing.
	Except []string
	// Volatile skips sockets whose connection cannot take the packet now.
	Volatile bool
	// Local restricts delivery to this instance without publishing to a cluster.
	Local bool
}

// Broadcast is the adaptor to handle broadcasts & rooms for socket.io server API
type Broadcast interface {
	// SendTo snapshots matching sockets and emits on the caller's goroutine after
	// releasing the adapter lock. Delivery uses target.Emit, or target.Volatile().Emit
	// when Volatile is set. An adapter unable to exclude returns an error wrapping
	// ErrExceptUnsupported before sending anything.
	SendTo(opts BroadcastOptions, event string, args ...interface{}) error
	// IDs returns a copy of the IDs in any of rooms, or all sockets when empty.
	IDs(rooms ...string) []string
	Join(room string, connection Conn)            // Join causes the connection to join a room
	Leave(room string, connection Conn)           // Leave causes the connection to leave a room
	LeaveAll(connection Conn)                     // LeaveAll causes given connection to leave all rooms
	Clear(room string)                            // Clear causes removal of all connections from the room
	Send(room, event string, args ...interface{}) // Send emits to a room; a trailing callback is registered per socket.
	SendAll(event string, args ...interface{})    // SendAll emits to all sockets; a trailing callback is registered per socket.
	ForEach(room string, f EachFunc)              // ForEach sends data by DataFunc, if room does not exits sends nothing
	Len(room string) int                          // Len gives number of connections in the room
	Rooms(connection Conn) []string               // Gives list of all the rooms if no connection given, else list of all the rooms the connection joined
	AllRooms() []string                           // Gives list of all the rooms the connection joined
}

// broadcast gives Join, Leave & BroadcastTO server API support to socket.io along with room management
// map of rooms where each room contains a map of connection id to connections in that room
type broadcast struct {
	rooms map[string]map[string]Conn
	sids  map[string]map[string]struct{}

	lock sync.RWMutex
}

// newBroadcast creates a new broadcast adapter
func newBroadcast() *broadcast {
	return &broadcast{
		rooms: make(map[string]map[string]Conn),
		sids:  make(map[string]map[string]struct{}),
	}
}

// Join joins the given connection to the broadcast room
func (bc *broadcast) Join(room string, connection Conn) {
	bc.lock.Lock()
	defer bc.lock.Unlock()

	if _, ok := bc.rooms[room]; !ok {
		bc.rooms[room] = make(map[string]Conn)
	}

	id := connection.ID()
	bc.rooms[room][id] = connection
	if bc.sids[id] == nil {
		bc.sids[id] = make(map[string]struct{})
	}
	bc.sids[id][room] = struct{}{}
}

// Leave leaves the given connection from given room (if exist)
func (bc *broadcast) Leave(room string, connection Conn) {
	bc.lock.Lock()
	defer bc.lock.Unlock()
	bc.leave(room, connection.ID())
}

// leave updates both indexes with the write lock held.
func (bc *broadcast) leave(room, id string) {
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
func (bc *broadcast) LeaveAll(connection Conn) {
	bc.lock.Lock()
	defer bc.lock.Unlock()
	id := connection.ID()
	for room := range bc.sids[id] {
		bc.leave(room, id)
	}
}

// Clear removes a room from both indexes.
func (bc *broadcast) Clear(room string) {
	bc.lock.Lock()
	defer bc.lock.Unlock()
	for id := range bc.rooms[room] {
		bc.leave(room, id)
	}
}

// Send emits to a room, registering a trailing callback on each socket.
func (bc *broadcast) Send(room, event string, args ...interface{}) {
	_ = bc.SendTo(BroadcastOptions{Rooms: []string{room}}, event, args...)
}

// SendAll emits once per socket, registering a trailing callback on each socket.
func (bc *broadcast) SendAll(event string, args ...interface{}) {
	_ = bc.SendTo(BroadcastOptions{}, event, args...)
}

func (bc *broadcast) SendTo(opts BroadcastOptions, event string, args ...interface{}) error {
	bc.lock.RLock()
	targets := broadcastMembers(bc.rooms, opts)
	bc.lock.RUnlock()
	deliverBroadcast(targets, opts, event, args...)
	return nil
}

func (bc *broadcast) IDs(rooms ...string) []string {
	bc.lock.RLock()
	defer bc.lock.RUnlock()
	if len(rooms) == 0 {
		ids := make([]string, 0, len(bc.sids))
		for id := range bc.sids {
			ids = append(ids, id)
		}
		return ids
	}
	return broadcastIDs(broadcastMembers(bc.rooms, BroadcastOptions{Rooms: rooms}))
}

// broadcastMembers takes a snapshot while the caller holds the room read lock.
func broadcastMembers(rooms map[string]map[string]Conn, opts BroadcastOptions) []Conn {
	seen := make(map[string]bool, len(opts.Except))
	for _, id := range opts.Except {
		seen[id] = true
	}
	var result []Conn
	add := func(members map[string]Conn) {
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

func broadcastIDs(targets []Conn) []string {
	ids := make([]string, 0, len(targets))
	for _, c := range targets {
		ids = append(ids, c.ID())
	}
	return ids
}

func deliverBroadcast(targets []Conn, opts BroadcastOptions, event string, args ...interface{}) {
	for _, c := range targets {
		if opts.Volatile {
			_ = c.Volatile().Emit(event, args...)
		} else {
			c.Emit(event, args...)
		}
	}
}

// ForEach sends data returned by DataFunc, if room does not exits sends nothing
func (bc *broadcast) ForEach(room string, f EachFunc) {
	for _, connection := range bc.members(room) {
		f(connection)
	}
}

// members returns the connections in room, so that callers emit without holding bc.lock.
func (bc *broadcast) members(room string) []Conn {
	bc.lock.RLock()
	defer bc.lock.RUnlock()

	occupants := bc.rooms[room]
	conns := make([]Conn, 0, len(occupants))
	for _, connection := range occupants {
		conns = append(conns, connection)
	}

	return conns
}

// Len gives number of connections in the room
func (bc *broadcast) Len(room string) int {
	bc.lock.RLock()
	defer bc.lock.RUnlock()

	return len(bc.rooms[room])
}

// Rooms gives the list of all the rooms available for broadcast in case of
// no connection is given, in case of a connection is given, it gives
// list of all the rooms the connection is joined to
func (bc *broadcast) Rooms(connection Conn) []string {
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

// AllRooms gives list of all rooms available for broadcast
func (bc *broadcast) AllRooms() []string {
	bc.lock.RLock()
	defer bc.lock.RUnlock()

	rooms := make([]string, 0, len(bc.rooms))
	for room := range bc.rooms {
		rooms = append(rooms, room)
	}

	return rooms
}
