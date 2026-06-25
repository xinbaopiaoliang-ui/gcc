package panel

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

const maxTrafficReportSamples = 50000

func (s *MySQLStore) GetTrafficOverview(ctx context.Context, filter TrafficOverviewFilter) (*TrafficOverview, error) {
	filter = normalizeTrafficFilter(filter)
	nodes, err := s.ListNodes(ctx, NodeListFilter{Limit: 10000})
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT node_id, version, route_policy_revision, metrics_json, reported_at
FROM panel_node_reports
WHERE reported_at >= ?
ORDER BY node_id ASC, reported_at ASC
LIMIT ?`, filter.Now.Add(-filter.Window), maxTrafficReportSamples)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	samples := make([]trafficReportSample, 0)
	for rows.Next() {
		var sample trafficReportSample
		var metricsRaw sql.NullString
		if err := rows.Scan(
			&sample.NodeID,
			&sample.Version,
			&sample.RoutePolicyRevision,
			&metricsRaw,
			&sample.ReportedAt,
		); err != nil {
			return nil, err
		}
		if metricsRaw.Valid && metricsRaw.String != "" {
			if err := json.Unmarshal([]byte(metricsRaw.String), &sample.Metrics); err != nil {
				return nil, fmt.Errorf("decode report metrics for node %s: %w", sample.NodeID, err)
			}
		}
		samples = append(samples, sample)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	overview := BuildTrafficOverview(nodes, samples, filter)
	return &overview, nil
}
