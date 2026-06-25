package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var ErrMetadataRequired = errors.New("flow metadata is required")

type FlowMetadata struct {
	GameID               string `json:"game_id,omitempty"`
	PolicyID             string `json:"policy_id,omitempty"`
	RuleID               string `json:"rule_id,omitempty"`
	Network              string `json:"network,omitempty"`
	ProcessName          string `json:"process_name,omitempty"`
	ProcessPathHash      string `json:"process_path_hash,omitempty"`
	ClientConfigRevision string `json:"client_config_revision,omitempty"`
	CaptureMode          string `json:"capture_mode,omitempty"`
	TraceID              string `json:"trace_id,omitempty"`
}

func ParseFlowMetadata(raw json.RawMessage) (FlowMetadata, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return FlowMetadata{}, nil
	}
	var metadata FlowMetadata
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return FlowMetadata{}, fmt.Errorf("invalid flow metadata: %w", err)
	}
	metadata.Normalize()
	return metadata, nil
}

func (m *FlowMetadata) Normalize() {
	m.GameID = strings.TrimSpace(m.GameID)
	m.PolicyID = strings.TrimSpace(m.PolicyID)
	m.RuleID = strings.TrimSpace(m.RuleID)
	m.Network = strings.ToLower(strings.TrimSpace(m.Network))
	m.ProcessName = strings.TrimSpace(m.ProcessName)
	m.ProcessPathHash = strings.TrimSpace(m.ProcessPathHash)
	m.ClientConfigRevision = strings.TrimSpace(m.ClientConfigRevision)
	m.CaptureMode = strings.TrimSpace(m.CaptureMode)
	m.TraceID = strings.TrimSpace(m.TraceID)
}

func (m FlowMetadata) Empty() bool {
	return m.GameID == "" &&
		m.PolicyID == "" &&
		m.RuleID == "" &&
		m.Network == "" &&
		m.ProcessName == "" &&
		m.ProcessPathHash == "" &&
		m.ClientConfigRevision == "" &&
		m.CaptureMode == "" &&
		m.TraceID == ""
}

func (m FlowMetadata) ValidateForNetwork(network string) error {
	network = strings.ToLower(strings.TrimSpace(network))
	if m.Empty() {
		return ErrMetadataRequired
	}
	if m.GameID == "" {
		return errors.New("metadata.game_id is required")
	}
	if m.PolicyID == "" {
		return errors.New("metadata.policy_id is required")
	}
	if m.RuleID == "" {
		return errors.New("metadata.rule_id is required")
	}
	if m.Network == "" {
		return errors.New("metadata.network is required")
	}
	if m.Network != network {
		return fmt.Errorf("metadata.network %q does not match %q", m.Network, network)
	}
	if m.ClientConfigRevision == "" {
		return errors.New("metadata.client_config_revision is required")
	}
	return nil
}
