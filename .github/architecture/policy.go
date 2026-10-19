package main

const modulePath = "github.com/sshaplygin/go-socket.io"

// allowedImports implements ROADMAP's Component boundaries table. Keys and values
// are module-relative package paths; the empty path denotes the Socket.IO server.
// These are allowed direct edges, not required imports. Test imports are separate.
var allowedImports = map[string][]string{
	"":                                 {"internal/connection", "internal/namespace", "adapter", "engineio", "logger", "internal/event"},
	"internal/connection":              {"internal/namespace", "adapter", "parser", "engineio", "engineio/parser", "logger", "internal/event"},
	"internal/namespace":               {"adapter", "parser", "engineio", "internal/event"},
	"client":                           {"client/internal/connection", "client/internal/namespace", "engineio/client", "engineio/transport", "engineio/transport/polling", "engineio/transport/websocket", "logger", "internal/event"},
	"client/internal/connection":       {"client/internal/namespace", "parser", "engineio/client", "engineio/parser", "engineio/transport", "engineio/transport/polling", "engineio/transport/websocket", "logger", "internal/event"},
	"client/internal/namespace":        {"parser", "engineio/client", "internal/event"},
	"internal/event":                   {"parser"},
	"adapter":                          {},
	"adapter/redis":                    {"adapter", "logger"},
	"parser":                           {"logger"},
	"engineio":                         {"engineio/internal/session", "engineio/parser", "engineio/transport", "engineio/transport/polling", "engineio/transport/websocket", "logger"},
	"engineio/internal/session":        {"engineio/parser", "engineio/transport", "engineio/transport/polling", "logger"},
	"engineio/client":                  {"engineio/parser", "engineio/transport", "engineio/transport/polling", "engineio/transport/websocket", "logger"},
	"engineio/parser":                  {"engineio/parser/internal/packet", "engineio/parser/internal/payload"},
	"engineio/parser/internal/packet":  {"logger"},
	"engineio/parser/internal/payload": {"engineio/parser/internal/packet"},
	"engineio/transport":               {"engineio/parser", "logger"},
	"engineio/transport/polling":       {"engineio/transport", "engineio/parser", "logger"},
	"engineio/transport/websocket":     {"engineio/transport", "engineio/parser", "logger"},
	"logger":                           {},
}

var retiredPaths = []string{
	"engineio/frame", "engineio/packet", "engineio/payload", "engineio/session",
	"engineio/transport/utils", "internal/engineclose", "engineio/internal/shutdown.go",
	"client/export_test.go",
}
