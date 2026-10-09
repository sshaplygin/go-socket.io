package engineio

// ConnChanLen returns the number of sessions in the hand-off buffer of s that
// no Accept has taken yet. The external engineio_test package cannot read
// Server.connChan.
func ConnChanLen(s *Server) int { return len(s.connChan) }
