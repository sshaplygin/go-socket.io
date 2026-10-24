package polling

import (
	"io"
	"strings"
	"testing"
	"time"
)

func BenchmarkPayloadRoundTrip(b *testing.B) {
	p := newPayload(false)
	b.Cleanup(func() { _ = p.Close() })
	if err := p.SetReadDeadline(time.Now().Add(45 * time.Second)); err != nil {
		b.Fatal(err)
	}
	if err := p.SetWriteDeadline(time.Now().Add(45 * time.Second)); err != nil {
		b.Fatal(err)
	}
	done := make(chan error, 1)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		go func() { done <- p.FeedIn(strings.NewReader("6:4hello"), false) }()
		_, _, r, err := p.NextReader()
		if err != nil {
			b.Fatal(err)
		}
		_, readErr := io.Copy(io.Discard, r)
		closeErr := r.Close()
		feedErr := <-done
		if readErr != nil {
			b.Fatal(readErr)
		}
		if closeErr != nil {
			b.Fatal(closeErr)
		}
		if feedErr != nil {
			b.Fatal(feedErr)
		}
	}
}
