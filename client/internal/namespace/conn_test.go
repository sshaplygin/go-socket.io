package namespace

// testConn supplies only the role identity; dispatch tests never call its methods.
type testConn struct{ Conn }
