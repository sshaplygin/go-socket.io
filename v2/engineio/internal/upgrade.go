package internal

import (
	"io"
	"time"
)

// DefaultUpgradeTimeout is the upgrade window when none is configured: the
// upgradeTimeout of the Node server.
const DefaultUpgradeTimeout = 10 * time.Second

// ReserveUpgrade takes the upgrade reservation of session, a *session.Session, before
// the connection of the upgrade is accepted. It reports false when the session is
// closed, or when an upgrade holds the reservation or has already switched the
// session. The reserved upgrade must receive the probe and UPGRADE within timeout of
// the accept. Package session sets it; engineio.Server calls it.
var ReserveUpgrade func(session io.Closer, timeout time.Duration) bool

// Closed reports whether session, a *session.Session, has been closed. Package session
// sets it; engineio.Server calls it.
var Closed func(session io.Closer) bool

// ReleaseUpgrade returns the reservation of an upgrade whose connection was not
// accepted. Package session sets it; engineio.Server calls it.
var ReleaseUpgrade func(session io.Closer)
