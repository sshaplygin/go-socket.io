package polling

import (
	"compress/gzip"
	"io"
)

// A BestSpeed writer keeps its flate working memory across Reset. Retaining a
// small process-wide set avoids allocating that memory for every large poll.
const maxRetainedGzipWriters = 4

type gzipWriterCache struct {
	free chan *gzip.Writer
}

func newGzipWriterCache() *gzipWriterCache {
	return &gzipWriterCache{free: make(chan *gzip.Writer, maxRetainedGzipWriters)}
}

var gzipWriters = newGzipWriterCache()

func (c *gzipWriterCache) take(dst io.Writer) (*gzip.Writer, error) {
	select {
	case writer := <-c.free:
		writer.Reset(dst)
		return writer, nil
	default:
		return gzip.NewWriterLevel(dst, gzip.BestSpeed)
	}
}

// put clears the previous response target before retaining a completed writer.
func (c *gzipWriterCache) put(writer *gzip.Writer) {
	writer.Reset(io.Discard)
	select {
	case c.free <- writer:
	default:
	}
}
