package socketio

import (
	"testing"
	"github.com/stretchr/testify/require"
)

// Covers: G23
func TestRegressionG23NilRoomConnection(t *testing.T) {
	s := NewServer()
	defer s.Close()
	s.Of("/")
	require.NotPanics(t, func() { require.False(t, s.JoinRoom("/", "room", nil)) })
}

