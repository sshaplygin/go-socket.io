package namespace

import (
	"sync"
)

// RoomIndex owns the client namespace's local room and socket membership indexes.
type RoomIndex struct {
	rooms map[string]map[string]Conn
	sids  map[string]map[string]struct{}

	lock sync.RWMutex
}

// NewRoomIndex creates an empty local room index.
func NewRoomIndex() *RoomIndex {
	return &RoomIndex{
		rooms: make(map[string]map[string]Conn),
		sids:  make(map[string]map[string]struct{}),
	}
}

// Join joins the given connection to the roomIndex room
func (bc *RoomIndex) Join(room string, connection Conn) {
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

// Leave removes the connection from the room, if joined.
func (bc *RoomIndex) Leave(room string, connection Conn) {
	bc.lock.Lock()
	defer bc.lock.Unlock()
	bc.leave(room, connection.ID())
}

// leave updates both indexes with the write lock held.
func (bc *RoomIndex) leave(room, id string) {
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
func (bc *RoomIndex) LeaveAll(connection Conn) {
	bc.lock.Lock()
	defer bc.lock.Unlock()
	id := connection.ID()
	for room := range bc.sids[id] {
		bc.leave(room, id)
	}
}

// Rooms returns a copy of this connection's joined room names.
func (bc *RoomIndex) Rooms(connection Conn) []string {

	bc.lock.RLock()
	defer bc.lock.RUnlock()

	rooms := make([]string, 0, len(bc.sids[connection.ID()]))
	for room := range bc.sids[connection.ID()] {
		rooms = append(rooms, room)
	}
	return rooms
}
