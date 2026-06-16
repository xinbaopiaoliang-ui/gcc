package panelcommand

import "sync"

const defaultResultHistoryLimit = 50

type ResultHistory struct {
	mu      sync.Mutex
	limit   int
	results []CommandResult
}

func NewResultHistory(limit int) *ResultHistory {
	if limit <= 0 {
		limit = defaultResultHistoryLimit
	}
	return &ResultHistory{limit: limit}
}

func (h *ResultHistory) Record(result CommandResult) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.results = append(h.results, result)
	if len(h.results) > h.limit {
		start := len(h.results) - h.limit
		h.results = append([]CommandResult(nil), h.results[start:]...)
	}
}

func (h *ResultHistory) Snapshot() []CommandResult {
	h.mu.Lock()
	defer h.mu.Unlock()

	results := make([]CommandResult, len(h.results))
	copy(results, h.results)
	return results
}
