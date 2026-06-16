package panelcommand

import (
	"testing"
	"time"
)

func TestResultHistoryKeepsLimitAndCopiesSnapshot(t *testing.T) {
	history := NewResultHistory(2)
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)

	history.Record(CommandResult{ID: "cmd-1", Type: CommandNoop, OK: true, ExecutedAt: now})
	history.Record(CommandResult{ID: "cmd-2", Type: CommandConfigReload, OK: true, ExecutedAt: now})
	history.Record(CommandResult{ID: "cmd-3", Type: CommandStageUpgrade, OK: true, ExecutedAt: now})

	snapshot := history.Snapshot()
	if len(snapshot) != 2 {
		t.Fatalf("len(snapshot) = %d, want 2", len(snapshot))
	}
	if snapshot[0].ID != "cmd-2" || snapshot[1].ID != "cmd-3" {
		t.Fatalf("snapshot IDs = %q, %q; want cmd-2, cmd-3", snapshot[0].ID, snapshot[1].ID)
	}

	snapshot[0].ID = "changed"
	again := history.Snapshot()
	if again[0].ID != "cmd-2" {
		t.Fatalf("snapshot mutation changed history: %q", again[0].ID)
	}
}
