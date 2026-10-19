package connection

import (
	"github.com/stretchr/testify/require"
	"testing"
)

// Covers G20
func TestClientVariadicEventsAndAcknowledgements(t *testing.T) {
	p := start(t, 'C', hooks{events: map[string]interface{}{"sum": func(_ Conn, values ...int) int {
		total := 0
		for _, value := range values {
			total += value
		}
		return total
	}}})
	p.send(t, `27["sum",1,2]`)
	require.Equal(t, "37[3]\n", recv(t, p.fc.out, "variadic event ACK"))
	p.send(t, `28["sum"]`)
	require.Equal(t, "38[0]\n", recv(t, p.fc.out, "zero variadic event ACK"))
	called := make(chan int, 1)
	p.nc.Emit("question", func(values ...int) {
		total := 0
		for _, value := range values {
			total += value
		}
		called <- total
	})
	require.Equal(t, "21[\"question\"]\n", recv(t, p.fc.out, "variadic ACK request"))
	p.send(t, `31[3,4]`)
	require.Equal(t, 7, recv(t, called, "variadic ACK result"))
}
