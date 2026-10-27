// Package polling implements the Engine.IO protocol v3 HTTP polling transport.
// It supports binary and base64 text payloads, HTTP compression and transport
// upgrades. Transport configures HTTP clients and origin checks; Engine.IO
// owns session routing, heartbeat and message limits.
package polling
