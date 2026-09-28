package logger

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// helperEnv makes the test binary act as a child process that prints what
// the package configured at init from SOCKETIO_LOG_LEVEL.
const helperEnv = "SOCKETIO_LOGGER_TEST_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		// Output on stdout: "<override> <level>". Warnings from init go to
		// stderr through the default slog handler.
		os.Stdout.WriteString(boolString(override.Load()) + " " + Level.Level().String() + "\n")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func runHelper(t *testing.T, value string, set bool) (stdout, stderr string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), helperEnv+"=1")
	if set {
		cmd.Env = append(cmd.Env, EnvLevel+"="+value)
	}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	require.NoError(t, cmd.Run())
	return strings.TrimSpace(out.String()), errOut.String()
}

func TestLogLevelFromEnv(t *testing.T) {
	cases := map[string]string{
		"error": "ERROR",
		"WARN":  "WARN",
		"Info":  "INFO",
		"debug": "DEBUG",
		"trace": LevelTrace.String(),
	}
	for value, want := range cases {
		out, errOut := runHelper(t, value, true)
		require.Equal(t, "true "+want, out, "value %q", value)
		require.Empty(t, errOut, "value %q", value)
	}

	out, _ := runHelper(t, "", false)
	require.Equal(t, "false INFO", out, "unset variable must not enable the override")
}

func TestLogLevelInvalidEnv(t *testing.T) {
	out, errOut := runHelper(t, "bogus", true)
	require.Equal(t, "false INFO", out, "invalid value must behave as unset")
	require.Equal(t, 1, strings.Count(errOut, "WARN"), "exactly one warning: %q", errOut)
	require.Contains(t, errOut, "bogus")
}

// setOverride enables the environment override at lvl for one test.
func setOverride(t *testing.T, lvl slog.Level) {
	t.Helper()
	prevOn, prevLvl := override.Load(), Level.Level()
	override.Store(true)
	Level.Set(lvl)
	t.Cleanup(func() {
		override.Store(prevOn)
		Level.Set(prevLvl)
	})
}

func TestWrapOverridesHandlerLevel(t *testing.T) {
	var buf bytes.Buffer
	app := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))
	l := Wrap(app)

	// Variable unset: the application's handler decides.
	l.Debug("hidden by app level")
	require.Empty(t, buf.String())

	// Variable set to debug: library lines pass even though the handler is at ERROR.
	setOverride(t, slog.LevelDebug)
	l.With("sid", "1").Debug("shown by override")
	require.Contains(t, buf.String(), "shown by override")
	require.Contains(t, buf.String(), "sid=1")

	// Trace stays below debug; ReplaceAttr renders its name.
	buf.Reset()
	l.Log(context.Background(), LevelTrace, "below debug")
	require.Empty(t, buf.String())

	setOverride(t, LevelTrace)
	named := Wrap(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{ReplaceAttr: ReplaceAttr})))
	named.Log(context.Background(), LevelTrace, "trace line")
	require.Contains(t, buf.String(), "level=TRACE")

	// Wrap is idempotent and nil-safe.
	require.Same(t, l, Wrap(l))
	require.NotNil(t, Wrap(nil))
}

func TestTraceDisabledNoAlloc(t *testing.T) {
	setOverride(t, slog.LevelDebug)
	l := Wrap(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))).With("sid", "1")
	ctx := context.Background()
	allocs := testing.AllocsPerRun(100, func() {
		if l.Enabled(ctx, LevelTrace) {
			l.Log(ctx, LevelTrace, "packet", "bytes", 1)
		}
	})
	require.Zero(t, allocs)
}

func TestErrorNilSafe(t *testing.T) {
	require.NotPanics(t, func() { Error("nil error", nil) })
}
