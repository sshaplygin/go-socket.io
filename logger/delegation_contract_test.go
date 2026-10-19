package logger

import (
	"bytes"
	"encoding/json"
	"log"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFallbackGroupsFollowDefaultHandler(t *testing.T) {
	setOverride(t, LevelUnset)
	previous, output, flags := slog.Default(), log.Writer(), log.Flags()
	t.Cleanup(func() {
		slog.SetDefault(previous)
		log.SetOutput(output)
		log.SetFlags(flags)
	})
	var first, second bytes.Buffer
	base := Log.With("sid", "root")
	connection := base.WithGroup("connection").With("transport", "polling")
	sibling := base.WithGroup("sibling")
	slog.SetDefault(slog.New(slog.NewJSONHandler(&first, nil)))
	connection.Info("first", "nsp", "/chat")
	sibling.Info("second", "value", 7)
	decoder := json.NewDecoder(&first)
	var a, b map[string]interface{}
	require.NoError(t, decoder.Decode(&a))
	require.NoError(t, decoder.Decode(&b))
	require.Equal(t, "root", a["sid"])
	require.Equal(t, map[string]interface{}{"transport": "polling", "nsp": "/chat"}, a["connection"])
	require.NotContains(t, a, "sibling")
	require.Equal(t, map[string]interface{}{"value": float64(7)}, b["sibling"])
	require.NotContains(t, b, "connection")
	slog.SetDefault(slog.New(slog.NewJSONHandler(&second, nil)))
	connection.Info("after replacement", "nsp", "/other")
	var next map[string]interface{}
	require.NoError(t, json.Unmarshal(second.Bytes(), &next))
	require.Equal(t, "after replacement", next["msg"])
	require.Equal(t, "root", next["sid"])
	require.Equal(t, map[string]interface{}{"transport": "polling", "nsp": "/other"}, next["connection"])
	require.Empty(t, first.Bytes(), "the old handler must receive no later records")
}
