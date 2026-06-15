package protocol

import (
	"encoding/json"
	"fmt"
	"io"
)

const Version = 1

const (
	ErrorAuthFailed             = "auth_failed"
	ErrorTokenExpired           = "token_expired"
	ErrorTokenNotActive         = "token_not_active"
	ErrorTokenMissingExp        = "token_missing_exp"
	ErrorTokenInvalid           = "token_invalid"
	ErrorUnauthorized           = "unauthorized"
	ErrorPermissionDenied       = "permission_denied"
	ErrorTargetDenied           = "target_denied"
	ErrorRateLimited            = "rate_limited"
	ErrorMaxFlowsExceeded       = "max_flows_exceeded"
	ErrorMaxConnectionsExceeded = "max_connections_exceeded"
	ErrorOpenUDPFailed          = "open_udp_failed"
	ErrorOpenTCPFailed          = "open_tcp_failed"
	ErrorUnknownMessage         = "unknown_message"
)

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
	Type           MessageType     `json:"type"`
	Version        int             `json:"version,omitempty"`
	Token          string          `json:"token,omitempty"`
	FlowID         uint32          `json:"flow_id,omitempty"`
	TargetHost     string          `json:"target_host,omitempty"`
	TargetPort     int             `json:"target_port,omitempty"`
	Network        string          `json:"network,omitempty"`
	ClientID       string          `json:"client_id,omitempty"`
	ClientVersion  string          `json:"client_version,omitempty"`
	ClientPlatform string          `json:"client_platform,omitempty"`
	UserID         string          `json:"user_id,omitempty"`
	DeviceID       string          `json:"device_id,omitempty"`
	Server         *ServerInfo     `json:"server,omitempty"`
	ErrorCode      string          `json:"error_code,omitempty"`
	Error          string          `json:"error,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
}

type ServerInfo struct {
	ALPN                            string   `json:"alpn"`
	ProtocolVersion                 int      `json:"protocol_version"`
	Capabilities                    []string `json:"capabilities"`
	KeepaliveIntervalSeconds        int      `json:"keepalive_interval_seconds"`
	DatagramHeaderBytes             int      `json:"datagram_header_bytes"`
	RecommendedDatagramBytes        int      `json:"recommended_datagram_bytes"`
	RecommendedDatagramPayloadBytes int      `json:"recommended_datagram_payload_bytes"`
	TokenPolicy                     string   `json:"token_policy"`
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
