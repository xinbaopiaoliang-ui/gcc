package protocol

import (
	"bytes"
	"testing"
)

func TestDatagramRoundTrip(t *testing.T) {
	payload := []byte("hello")
	packet := MarshalDatagram(Datagram{
		Version: Version,
		Type:    DatagramTypeUDP,
		FlowID:  42,
		Seq:     7,
		Flags:   1,
		Payload: payload,
	})

	got, err := ParseDatagram(packet)
	if err != nil {
		t.Fatalf("ParseDatagram returned error: %v", err)
	}
	if got.Version != Version {
		t.Fatalf("Version = %d, want %d", got.Version, Version)
	}
	if got.Type != DatagramTypeUDP {
		t.Fatalf("Type = %d, want %d", got.Type, DatagramTypeUDP)
	}
	if got.FlowID != 42 {
		t.Fatalf("FlowID = %d, want 42", got.FlowID)
	}
	if got.Seq != 7 {
		t.Fatalf("Seq = %d, want 7", got.Seq)
	}
	if got.Flags != 1 {
		t.Fatalf("Flags = %d, want 1", got.Flags)
	}
	if !bytes.Equal(got.Payload, payload) {
		t.Fatalf("Payload = %q, want %q", got.Payload, payload)
	}
}

func TestParseDatagramRejectsShortPacket(t *testing.T) {
	if _, err := ParseDatagram([]byte{1, 2, 3}); err == nil {
		t.Fatal("ParseDatagram returned nil error for short packet")
	}
}
