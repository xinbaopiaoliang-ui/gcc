package protocol

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestParseFlowMetadataNormalizesFields(t *testing.T) {
	raw := json.RawMessage(`{"game_id":" steam ","policy_id":"steam-web-v1","rule_id":"rule-1","network":"TCP","client_config_revision":" r1 "}`)
	metadata, err := ParseFlowMetadata(raw)
	if err != nil {
		t.Fatalf("ParseFlowMetadata returned error: %v", err)
	}
	if metadata.GameID != "steam" {
		t.Fatalf("GameID = %q, want steam", metadata.GameID)
	}
	if metadata.Network != "tcp" {
		t.Fatalf("Network = %q, want tcp", metadata.Network)
	}
	if metadata.ClientConfigRevision != "r1" {
		t.Fatalf("ClientConfigRevision = %q, want r1", metadata.ClientConfigRevision)
	}
}

func TestFlowMetadataValidateRequiresFields(t *testing.T) {
	var metadata FlowMetadata
	if err := metadata.ValidateForNetwork("tcp"); !errors.Is(err, ErrMetadataRequired) {
		t.Fatalf("ValidateForNetwork error = %v, want ErrMetadataRequired", err)
	}
}

func TestFlowMetadataValidateChecksNetwork(t *testing.T) {
	metadata := FlowMetadata{
		GameID:               "steam",
		PolicyID:             "steam-web-v1",
		RuleID:               "rule-1",
		Network:              "udp",
		ClientConfigRevision: "r1",
	}
	if err := metadata.ValidateForNetwork("tcp"); err == nil {
		t.Fatal("ValidateForNetwork returned nil for network mismatch")
	}
}
