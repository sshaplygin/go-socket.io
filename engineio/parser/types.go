package parser

type Frame struct {
	FType FrameType
	Data  []byte
}

type Packet struct {
	FType FrameType
	PType PacketType
	Data  []byte
}
