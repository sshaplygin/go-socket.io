package engineio

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/googollee/go-socket.io/logger"
)

// TestOptionsLoggerWrapped checks that Options.Logger is passed through
// logger.Wrap: with logger.Level at debug, a debug record is enabled even
// though the application's handler is at ERROR. A nil Logger falls back to
// logger.Log.
func TestOptionsLoggerWrapped(t *testing.T) {
	prev := logger.Level.Level()
	logger.Level.Set(slog.LevelDebug)
	t.Cleanup(func() { logger.Level.Set(prev) })

	app := slog.New(slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelError}))
	opts := &Options{Logger: app}
	require.True(t, opts.getLogger().Enabled(context.Background(), slog.LevelDebug))

	var nilOpts *Options
	require.Same(t, logger.Log, nilOpts.getLogger())
	require.Same(t, logger.Log, (&Options{}).getLogger())
}
