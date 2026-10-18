package parser

import (
	"encoding/json"
	"os"
	"testing"
)

type payloadVectorPacket struct {
	ft   FrameType
	pt   PacketType
	data []byte
}
type payloadVector struct {
	supportBinary bool
	data          []byte
	packets       []payloadVectorPacket
}

var payloadVectors []payloadVector

func TestMain(m *testing.M) {
	data, err := os.ReadFile("testdata/payload.json")
	if err != nil {
		panic(err)
	}
	var corpus []struct {
		Binary  bool
		Data    []byte
		Packets []struct {
			Frame FrameType
			Type  PacketType
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
