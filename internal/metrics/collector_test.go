package metrics

import "testing"

func TestUDPDatagramDroppedRecordsFlowEvent(t *testing.T) {
	collector := NewCollector()

	collector.UDPDatagramDropped("send_queue_overflow", "steam", "steam-game")
	collector.UDPDatagramDropped("send_queue_overflow", "steam", "steam-game")

	events := collector.Snapshot().FlowEvents
	if len(events) != 1 {
		t.Fatalf("flow events = %d, want 1: %#v", len(events), events)
	}
	event := events[0]
	if event.Network != "udp" || event.Event != "drop" || event.Reason != "send_queue_overflow" {
		t.Fatalf("unexpected event identity: %#v", event)
	}
	if event.GameID != "steam" || event.PolicyID != "steam-game" {
		t.Fatalf("unexpected event policy labels: %#v", event)
	}
	if event.Count != 2 {
		t.Fatalf("event count = %d, want 2", event.Count)
	}
}
