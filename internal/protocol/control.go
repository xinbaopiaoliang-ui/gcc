package protocol

import (
	"encoding/json"
	"fmt"
	"io"
)

const Version = 1

type MessageType string

const (
	MessageHello   MessageType = "HELLO"
	MessageAuth    MessageType = "AUTH"
	MessageAuthOK  MessageType = "AUTH_OK"
	MessageOpenUDP MessageType = "OPEN_UDP"
	MessageOpenTCP MessageType = "OPEN_TCP"
	MessageClose   MessageType = "CLOSE_FLOW"
	MessagePing    MessageType = "PING"
	MessagePong    MessageType = "PONG"
	MessageError   MessageType = "ERROR"
	MessageStats   MessageType = "STATS"
)

type Message struct {
	Type       MessageType     `json:"type"`
	Version    int             `json:"version,omitempty"`
	Token      string          `json:"token,omitempty"`
	FlowID     uint32          `json:"flow_id,omitempty"`
	TargetHost string          `json:"target_host,omitempty"`
	TargetPort int             `json:"target_port,omitempty"`
	Network    string          `json:"network,omitempty"`
	ErrorCode  string          `json:"error_code,omitempty"`
	Error      string          `json:"error,omitempty"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
}

type Codec struct {
	decoder *json.Decoder
	encoder *json.Encoder
}

func NewCodec(rw io.ReadWriter) *Codec {
	return &Codec{
		decoder: json.NewDecoder(rw),
		encoder: json.NewEncoder(rw),
	}
}

func (c *Codec) Read() (*Message, error) {
	var msg Message
	if err := c.decoder.Decode(&msg); err != nil {
		return nil, err
	}
	if msg.Type == "" {
		return nil, fmt.Errorf("message type is required")
	}
	return &msg, nil
}

func (c *Codec) Write(msg Message) error {
	return c.encoder.Encode(msg)
}

func ErrorMessage(code, text string) Message {
	return Message{
		Type:      MessageError,
		ErrorCode: code,
		Error:     text,
	}
}
