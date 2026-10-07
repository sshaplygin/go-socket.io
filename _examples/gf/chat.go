// Chat server logic shared by every example. It ports index.js of the Socket.IO chat example,
// https://github.com/socketio/socket.io/blob/7a70f63499e2be66b072543db8ebf928b6923633/examples/chat/index.js
// (MIT license, see asset/LICENSE-socket.io-chat), to this library.
//
// This file is byte-identical in every example module; "make examples" fails when a copy differs.
package main

import (
	"log"
	"sync"

	socketio "github.com/sshaplygin/go-socket.io"
)

const chatRoom = "chat"

// chatUser is the connection context. Its fields are guarded by the mutex of registerChat.
type chatUser struct {
	name  string
	added bool // the connection sent "add user"
}

// userPayload is the payload of "typing" and "stop typing". Like upstream, a connection that
// has not added a user name sends no username.
type userPayload struct {
	Username string `json:"username,omitempty"`
}

// countPayload is the payload of "login", "user joined" and "user left".
type countPayload struct {
	Username string `json:"username,omitempty"`
	NumUsers int    `json:"numUsers"`
}

// messagePayload is the payload of "new message".
type messagePayload struct {
	Username string `json:"username,omitempty"`
	Message  string `json:"message"`
}

// registerChat registers the chat handlers on namespace "/".
//
// Server.ForEach reaches only the connections of this instance, so with the Redis adapter users
// on different instances do not see each other's events and each instance counts its own users.
func registerChat(server *socketio.Server) {
	var (
		mu       sync.Mutex // guards numUsers and every chatUser
		numUsers int
	)

	userOf := func(s socketio.Conn) *chatUser {
		if u, ok := s.Context().(*chatUser); ok {
			return u
		}
		return &chatUser{}
	}

	// others sends the event to every connection of the chat room except s: the library has no
	// broadcast that skips the sender.
	others := func(s socketio.Conn, event string, payload interface{}) {
		server.ForEach("/", chatRoom, func(c socketio.Conn) {
			if c.ID() != s.ID() {
				c.Emit(event, payload)
			}
		})
	}

	server.OnConnect("/", func(s socketio.Conn) error {
		// A Go client session runs OnConnect for "/" twice; keep the user of the first run.
		if _, ok := s.Context().(*chatUser); !ok {
			s.SetContext(&chatUser{})
		}
		s.Join(chatRoom)
		return nil
	})

	// "add user" announces the connection under a user name; a second one is ignored.
	server.OnEvent("/", "add user", func(s socketio.Conn, name string) {
		mu.Lock()
		defer mu.Unlock()

		u := userOf(s)
		if u.added {
			return
		}
		u.name, u.added = name, true
		numUsers++

		s.Emit("login", countPayload{NumUsers: numUsers})
		others(s, "user joined", countPayload{Username: name, NumUsers: numUsers})
	})

	server.OnEvent("/", "new message", func(s socketio.Conn, message string) {
		mu.Lock()
		defer mu.Unlock()

		others(s, "new message", messagePayload{Username: userOf(s).name, Message: message})
	})

	server.OnEvent("/", "typing", func(s socketio.Conn) {
		mu.Lock()
		defer mu.Unlock()

		others(s, "typing", userPayload{Username: userOf(s).name})
	})

	server.OnEvent("/", "stop typing", func(s socketio.Conn) {
		mu.Lock()
		defer mu.Unlock()

		others(s, "stop typing", userPayload{Username: userOf(s).name})
	})

	server.OnDisconnect("/", func(s socketio.Conn, reason string) {
		mu.Lock()
		defer mu.Unlock()

		u := userOf(s)
		if !u.added {
			return
		}
		u.added = false
		numUsers--

		others(s, "user left", countPayload{Username: u.name, NumUsers: numUsers})
	})

	server.OnError("/", func(s socketio.Conn, err error) {
		log.Println("chat error:", err)
	})
}
