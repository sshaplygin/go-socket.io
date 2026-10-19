package socketio

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNamespaceErrorMessageKeepsDiagnosticContext(t *testing.T) {
	cause := errors.New("adapter unavailable")
	var err error = newErrorMessage("/chat", cause)
	require.EqualError(t, err, "error in namespace: (/chat) with error: (adapter unavailable)")
}
