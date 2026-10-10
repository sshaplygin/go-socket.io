package socketio

import "github.com/google/uuid"

func newV4UUID() string {
	return uuid.NewString()
}
