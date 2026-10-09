package payload

import (
	"bytes"
	"io"

	"github.com/sshaplygin/go-socket.io/engineio/frame"
	"github.com/sshaplygin/go-socket.io/engineio/packet"
)

type readerFeeder interface {
	getReader() ([]Packet, error)
	putReader(error) error
}

// decoder hands out the packets of the batch that FeedIn decoded, one at a time.
// Closing the reader of the last packet releases FeedIn.
type decoder struct {
	feeder  readerFeeder
	packets []Packet
	data    bytes.Reader
}

func (d *decoder) NextReader() (frame.Type, packet.Type, io.ReadCloser, error) {
	if len(d.packets) == 0 {
		packets, err := d.feeder.getReader()
		if err != nil {
			return 0, 0, nil, err
		}
		d.packets = packets
	}

	first := d.packets[0]
	d.data.Reset(first.Data)
	return first.Frame, first.Type, d, nil
}

func (d *decoder) Read(p []byte) (int, error) {
	return d.data.Read(p)
}

// Close discards the rest of the current packet and moves to the next one. After
// the last packet of the body it releases FeedIn.
func (d *decoder) Close() error {
	if len(d.packets) == 0 {
		return nil
	}
	d.data.Reset(nil)
	d.packets = d.packets[1:]
	if len(d.packets) > 0 {
		return nil
	}
	d.packets = nil
	return d.feeder.putReader(nil)
}
