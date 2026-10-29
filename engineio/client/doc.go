// Package client implements an Engine.IO protocol v3 client.
// Dialer opens polling or websocket connections, negotiates upgrades and applies
// heartbeat and message limits. Socket.IO applications normally use the module
// root client package instead of exchanging Engine.IO packets directly.
package client
