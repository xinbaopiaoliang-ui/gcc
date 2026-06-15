package protocol

import (
	"encoding/binary"
	"errors"
)

const DatagramHeaderLen = 11
const RecommendedDatagramBytes = 1200
const RecommendedDatagramPayloadBytes = RecommendedDatagramBytes - DatagramHeaderLen

const (
	DatagramTypeUDP byte = 1
)

type Datagram struct {
	Version byte
	Type    byte
	FlowID  uint32
	Seq     uint32
	Flags   byte
	Payload []byte
}

func ParseDatagram(packet []byte) (*Datagram, error) {
	if len(packet) < DatagramHeaderLen {
		return nil, errors.New("datagram too short")
	}
	return &Datagram{
		Version: packet[0],
		Type:    packet[1],
		FlowID:  binary.BigEndian.Uint32(packet[2:6]),
		Seq:     binary.BigEndian.Uint32(packet[6:10]),
		Flags:   packet[10],
		Payload: packet[11:],
	}, nil
}

func MarshalDatagram(d Datagram) []byte {
	packet := make([]byte, DatagramHeaderLen+len(d.Payload))
	packet[0] = d.Version
	packet[1] = d.Type
	binary.BigEndian.PutUint32(packet[2:6], d.FlowID)
	binary.BigEndian.PutUint32(packet[6:10], d.Seq)
	packet[10] = d.Flags
	copy(packet[11:], d.Payload)
	return packet
}
