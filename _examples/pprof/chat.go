// Chat server logic shared by every example. It ports index.js of the Socket.IO chat example,
// https://github.com/socketio/socket.io/blob/7a70f63499e2be66b072543db8ebf928b6923633/examples/chat/index.js
// (MIT license, see asset/LICENSE-socket.io-chat), to this library.
//
// This file is byte-identical in all chat examples; "make examples" checks those copies.
package main

import (
	"log"
	"sync"

	socketio "github.com/sshaplygin/go-socket.io"
)

const chatRoom = "chat"
const chatUsersRoom = "chat-users"

// chatUser is the connection context. Its fields are guarded by the mutex of registerChat.
type chatUser struct {
	name  *string // nil until "add user"; like upstream, "" is a defined name
	added bool    // the connection sent "add user"
}

// userPayload is the payload of "typing" and "stop typing". Like upstream, where JSON.stringify
// drops only an undefined username, a connection that has not added a user name sends no
// username, while an empty name is sent as "".
type userPayload struct {
	Username *string `json:"username,omitempty"`
}

// countPayload is the payload of "login", "user joined" and "user left".
type countPayload struct {
	Username *string `json:"username,omitempty"`
	NumUsers int     `json:"numUsers"`
}

// messagePayload is the payload of "new message".
type messagePayload struct {
	Username *string `json:"username,omitempty"`
	Message  string  `json:"message"`
}

// registerChat registers the chat handlers on namespace "/".
func registerChat(server *socketio.Server) {
	var (
		mu sync.Mutex // guards each local chatUser
	)

	userOf := func(s socketio.Conn) *chatUser {
		if u, ok := s.Context().(*chatUser); ok {
			return u
		}
		return &chatUser{}
	}

	server.OnConnect("/", func(s socketio.Conn) error {
		// Keep the per-socket chat state in the connection context.
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
		u.name, u.added = &name, true
		s.Join(chatUsersRoom)
		numUsers := server.RoomLen("/", chatUsersRoom)

		s.Emit("login", countPayload{NumUsers: numUsers})
		_ = s.To(chatRoom).Emit("user joined", countPayload{Username: &name, NumUsers: numUsers})
	})

	server.OnEvent("/", "new message", func(s socketio.Conn, message string) {
		mu.Lock()
		defer mu.Unlock()

		_ = s.To(chatRoom).Emit("new message", messagePayload{Username: userOf(s).name, Message: message})
	})

	server.OnEvent("/", "typing", func(s socketio.Conn) {
		mu.Lock()
		defer mu.Unlock()

		_ = s.To(chatRoom).Emit("typing", userPayload{Username: userOf(s).name})
	})

	server.OnEvent("/", "stop typing", func(s socketio.Conn) {
		mu.Lock()
		defer mu.Unlock()

		_ = s.To(chatRoom).Emit("stop typing", userPayload{Username: userOf(s).name})
	})

	server.OnDisconnect("/", func(s socketio.Conn, reason string) {
		mu.Lock()
		defer mu.Unlock()

		u := userOf(s)
		if !u.added {
			return
		}
		u.added = false
		s.Leave(chatUsersRoom)
		numUsers := server.RoomLen("/", chatUsersRoom)

		_ = s.To(chatRoom).Emit("user left", countPayload{Username: u.name, NumUsers: numUsers})
	})

	server.OnError("/", func(s socketio.Conn, err error) {
		log.Println("chat error:", err)
	})
}
