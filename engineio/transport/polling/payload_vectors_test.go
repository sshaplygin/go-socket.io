package polling

import (
	"encoding/json"
	"os"
	"testing"

	eioparser "github.com/sshaplygin/go-socket.io/engineio/parser"
)

type payloadVectorPacket struct {
	ft   eioparser.FrameType
	pt   eioparser.PacketType
	data []byte
}
type payloadVector struct {
	supportBinary bool
	data          []byte
	packets       []payloadVectorPacket
}

var payloadVectors []payloadVector

func TestMain(m *testing.M) {
	data, err := os.ReadFile("../../parser/testdata/payload.json")
	if err != nil {
		panic(err)
	}
	var corpus []struct {
		Binary  bool
		Data    []byte
		Packets []struct {
			Frame eioparser.FrameType
			Type  eioparser.PacketType
			Data  []byte
		}
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		panic(err)
	}
	for _, v := range corpus {
		w := payloadVector{supportBinary: v.Binary, data: v.Data}
		for _, p := range v.Packets {
			w.packets = append(w.packets, payloadVectorPacket{p.Frame, p.Type, p.Data})
		}
		payloadVectors = append(payloadVectors, w)
	}
	os.Exit(m.Run())
}
