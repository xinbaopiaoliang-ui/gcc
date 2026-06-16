package config

import "testing"

func TestNormalizeNodeMetadata(t *testing.T) {
	node := NodeConfig{
		ID:     " node-hk-01 ",
		Region: " hk ",
		Tags:   []string{" steam ", "", "quic", "steam"},
		Labels: map[string]string{
			" provider ": " example ",
			"empty":      "",
			"":           "ignored",
		},
	}

	normalizeNode(&node)

	if node.ID != "node-hk-01" {
		t.Fatalf("ID = %q, want node-hk-01", node.ID)
	}
	if node.Region != "hk" {
		t.Fatalf("Region = %q, want hk", node.Region)
	}
	if len(node.Tags) != 2 || node.Tags[0] != "steam" || node.Tags[1] != "quic" {
		t.Fatalf("Tags = %#v, want [steam quic]", node.Tags)
	}
	if got := node.Labels["provider"]; got != "example" {
		t.Fatalf("Labels[provider] = %q, want example", got)
	}
	if _, ok := node.Labels["empty"]; ok {
		t.Fatal("empty label value was not removed")
	}
}
