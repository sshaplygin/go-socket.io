// Package engineio implements an Engine.IO protocol v3 server.
// A Server handles polling and websocket HTTP requests and yields accepted
// connections to its caller. Options configure heartbeat, transports, origin
// checks and resource limits; Close releases its sessions.
package engineio
