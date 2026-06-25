package panel

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"gaccel-node/internal/metrics"
)

const (
	defaultTrafficWindow = 24 * time.Hour
	maxTrafficWindow     = 7 * 24 * time.Hour
	defaultTrafficLimit  = 20
	maxTrafficLimit      = 100
)

type TrafficOverviewFilter struct {
	Window time.Duration
	Limit  int
	Now    time.Time
}

type TrafficOverview struct {
	WindowSeconds     int64                      `json:"window_seconds"`
	WindowStartedAt   time.Time                  `json:"window_started_at"`
	GeneratedAt       time.Time                  `json:"generated_at"`
	SampleMode        string                     `json:"sample_mode"`
	Totals            TrafficTotals              `json:"totals"`
	Nodes             []TrafficNodeStats         `json:"nodes"`
	Users             []TrafficUserStats         `json:"users"`
	FlowEvents        []TrafficFlowEventStats    `json:"flow_events"`
	PolicyEvents      []TrafficPolicyEventStats  `json:"policy_events"`
	PolicyConsistency []TrafficPolicyConsistency `json:"policy_consistency"`
	Recommendations   []string                   `json:"recommendations"`
}

type TrafficTotals struct {
	NodeCount             int   `json:"node_count"`
	OnlineNodeCount       int   `json:"online_node_count"`
	ReportNodeCount       int   `json:"report_node_count"`
	ActiveQUICConnections int64 `json:"active_quic_connections"`
	ActiveTCPFlows        int64 `json:"active_tcp_flows"`
	ActiveUDPFlows        int64 `json:"active_udp_flows"`
	TCPClientToTarget     int64 `json:"tcp_client_to_target_bytes"`
	TCPTargetToClient     int64 `json:"tcp_target_to_client_bytes"`
	UDPClientToTarget     int64 `json:"udp_client_to_target_bytes"`
	UDPTargetToClient     int64 `json:"udp_target_to_client_bytes"`
	TotalBytes            int64 `json:"total_bytes"`
	FlowOpenErrors        int64 `json:"flow_open_errors"`
	FlowCloseEvents       int64 `json:"flow_close_events"`
	PolicyDriftNodes      int   `json:"policy_drift_nodes"`
}

type TrafficByteBreakdown struct {
	TCPClientToTarget int64 `json:"tcp_client_to_target_bytes"`
	TCPTargetToClient int64 `json:"tcp_target_to_client_bytes"`
	UDPClientToTarget int64 `json:"udp_client_to_target_bytes"`
	UDPTargetToClient int64 `json:"udp_target_to_client_bytes"`
	TotalBytes        int64 `json:"total_bytes"`
}

type TrafficNodeStats struct {
	NodeID                string               `json:"node_id"`
	Name                  string               `json:"name"`
	Region                string               `json:"region"`
	Endpoint              string               `json:"endpoint"`
	Status                string               `json:"status"`
	CurrentVersion        string               `json:"current_version"`
	DesiredVersion        string               `json:"desired_version"`
	CurrentPolicyRevision string               `json:"current_policy_revision"`
	DesiredPolicyRevision string               `json:"desired_policy_revision"`
	PolicyState           string               `json:"policy_state"`
	ReportCount           int                  `json:"report_count"`
	LatestReportAt        *time.Time           `json:"latest_report_at,omitempty"`
	ReportAgeSeconds      int64                `json:"report_age_seconds"`
	ActiveQUICConnections int64                `json:"active_quic_connections"`
	ActiveTCPFlows        int64                `json:"active_tcp_flows"`
	ActiveUDPFlows        int64                `json:"active_udp_flows"`
	Traffic               TrafficByteBreakdown `json:"traffic"`
	LastError             string               `json:"last_error"`
	Labels                map[string]string    `json:"labels,omitempty"`
	Tags                  []string             `json:"tags,omitempty"`
}

type TrafficUserStats struct {
	UserID            string               `json:"user_id"`
	ActiveConnections int64                `json:"active_connections"`
	Traffic           TrafficByteBreakdown `json:"traffic"`
	NodeCount         int                  `json:"node_count"`
}

type TrafficFlowEventStats struct {
	Network  string `json:"network"`
	Event    string `json:"event"`
	Reason   string `json:"reason"`
	GameID   string `json:"game_id,omitempty"`
	PolicyID string `json:"policy_id,omitempty"`
	Count    int64  `json:"count"`
}

type TrafficPolicyEventStats struct {
	GameID   string `json:"game_id,omitempty"`
	PolicyID string `json:"policy_id,omitempty"`
	Network  string `json:"network"`
	Open     int64  `json:"open"`
	Close    int64  `json:"close"`
	Error    int64  `json:"error"`
	Total    int64  `json:"total"`
}

type TrafficPolicyConsistency struct {
	NodeID                string     `json:"node_id"`
	Name                  string     `json:"name"`
	CurrentPolicyRevision string     `json:"current_policy_revision"`
	DesiredPolicyRevision string     `json:"desired_policy_revision"`
	State                 string     `json:"state"`
	LastReportAt          *time.Time `json:"last_report_at,omitempty"`
	ReportAgeSeconds      int64      `json:"report_age_seconds"`
	LastError             string     `json:"last_error"`
}

type trafficReportSample struct {
	NodeID              string
	Version             string
	RoutePolicyRevision string
	Metrics             metrics.Snapshot
	ReportedAt          time.Time
}

func normalizeTrafficFilter(filter TrafficOverviewFilter) TrafficOverviewFilter {
	if filter.Window <= 0 {
		filter.Window = defaultTrafficWindow
	}
	if filter.Window > maxTrafficWindow {
		filter.Window = maxTrafficWindow
	}
	if filter.Limit <= 0 {
		filter.Limit = defaultTrafficLimit
	}
	if filter.Limit > maxTrafficLimit {
		filter.Limit = maxTrafficLimit
	}
	if filter.Now.IsZero() {
		filter.Now = time.Now().UTC()
	} else {
		filter.Now = filter.Now.UTC()
	}
	return filter
}

func BuildTrafficOverview(nodes []Node, samples []trafficReportSample, filter TrafficOverviewFilter) TrafficOverview {
	filter = normalizeTrafficFilter(filter)
	windowStartedAt := filter.Now.Add(-filter.Window)
	nodeByID := make(map[string]Node, len(nodes))
	for _, node := range nodes {
		nodeByID[node.NodeID] = node
	}

	samplesByNode := make(map[string][]trafficReportSample)
	for _, sample := range samples {
		if strings.TrimSpace(sample.NodeID) == "" {
			continue
		}
		if sample.ReportedAt.IsZero() {
			sample.ReportedAt = filter.Now
		}
		samplesByNode[sample.NodeID] = append(samplesByNode[sample.NodeID], sample)
	}
	for nodeID := range samplesByNode {
		sort.Slice(samplesByNode[nodeID], func(i, j int) bool {
			return samplesByNode[nodeID][i].ReportedAt.Before(samplesByNode[nodeID][j].ReportedAt)
		})
	}

	overview := TrafficOverview{
		WindowSeconds:   int64(filter.Window.Seconds()),
		WindowStartedAt: windowStartedAt,
		GeneratedAt:     filter.Now,
		SampleMode:      "latest_cumulative",
	}
	overview.Totals.NodeCount = len(nodes)

	userAgg := make(map[string]*trafficUserAgg)
	eventAgg := make(map[string]*TrafficFlowEventStats)
	policyAgg := make(map[string]*TrafficPolicyEventStats)
	nodeIDs := make([]string, 0, len(nodeByID))
	for nodeID := range nodeByID {
		nodeIDs = append(nodeIDs, nodeID)
	}
	for nodeID := range samplesByNode {
		if _, ok := nodeByID[nodeID]; !ok {
			nodeIDs = append(nodeIDs, nodeID)
		}
	}
	sort.Strings(nodeIDs)

	for _, nodeID := range nodeIDs {
		node := nodeByID[nodeID]
		nodeSamples := samplesByNode[nodeID]
		if node.Status == "online" {
			overview.Totals.OnlineNodeCount++
		}
		nodeStat := TrafficNodeStats{
			NodeID:                firstNonEmpty(node.NodeID, nodeID),
			Name:                  node.Name,
			Region:                node.Region,
			Endpoint:              endpointLabel(node.EndpointHost, node.EndpointPort),
			Status:                node.Status,
			CurrentVersion:        node.CurrentVersion,
			DesiredVersion:        node.DesiredVersion,
			CurrentPolicyRevision: node.CurrentPolicyRevision,
			DesiredPolicyRevision: node.DesiredPolicyRevision,
			PolicyState:           policyState(node.CurrentPolicyRevision, node.DesiredPolicyRevision),
			LastError:             node.LastError,
			Labels:                node.Labels,
			Tags:                  node.Tags,
		}
		if node.LastReportAt != nil {
			t := node.LastReportAt.UTC()
			nodeStat.LatestReportAt = &t
			nodeStat.ReportAgeSeconds = int64(filter.Now.Sub(t).Seconds())
		}
		if len(nodeSamples) > 0 {
			overview.Totals.ReportNodeCount++
			first := nodeSamples[0]
			latest := nodeSamples[len(nodeSamples)-1]
			nodeStat.ReportCount = len(nodeSamples)
			reportedAt := latest.ReportedAt.UTC()
			nodeStat.LatestReportAt = &reportedAt
			nodeStat.ReportAgeSeconds = int64(filter.Now.Sub(reportedAt).Seconds())
			nodeStat.CurrentVersion = firstNonEmpty(nodeStat.CurrentVersion, latest.Version)
			nodeStat.CurrentPolicyRevision = firstNonEmpty(nodeStat.CurrentPolicyRevision, latest.RoutePolicyRevision)
			nodeStat.PolicyState = policyState(nodeStat.CurrentPolicyRevision, nodeStat.DesiredPolicyRevision)
			nodeStat.ActiveQUICConnections = latest.Metrics.ActiveQUICConnections
			nodeStat.ActiveTCPFlows = latest.Metrics.ActiveTCPFlows
			nodeStat.ActiveUDPFlows = latest.Metrics.ActiveUDPFlows
			nodeStat.Traffic = metricTrafficDelta(first.Metrics, latest.Metrics, len(nodeSamples) > 1)
			if len(nodeSamples) > 1 {
				overview.SampleMode = "window_delta"
			}
			overview.Totals.ActiveQUICConnections += latest.Metrics.ActiveQUICConnections
			overview.Totals.ActiveTCPFlows += latest.Metrics.ActiveTCPFlows
			overview.Totals.ActiveUDPFlows += latest.Metrics.ActiveUDPFlows
			addTrafficTotals(&overview.Totals, nodeStat.Traffic)
			aggregateUsers(userAgg, nodeID, first.Metrics, latest.Metrics, len(nodeSamples) > 1)
			aggregateFlowEvents(eventAgg, policyAgg, first.Metrics, latest.Metrics, len(nodeSamples) > 1)
		}
		if nodeStat.PolicyState == "pending" {
			overview.Totals.PolicyDriftNodes++
		}
		overview.Nodes = append(overview.Nodes, nodeStat)
		overview.PolicyConsistency = append(overview.PolicyConsistency, TrafficPolicyConsistency{
			NodeID:                nodeStat.NodeID,
			Name:                  nodeStat.Name,
			CurrentPolicyRevision: nodeStat.CurrentPolicyRevision,
			DesiredPolicyRevision: nodeStat.DesiredPolicyRevision,
			State:                 nodeStat.PolicyState,
			LastReportAt:          nodeStat.LatestReportAt,
			ReportAgeSeconds:      nodeStat.ReportAgeSeconds,
			LastError:             nodeStat.LastError,
		})
	}

	for _, row := range eventAgg {
		if row.Event == "open" && row.Reason != "success" {
			overview.Totals.FlowOpenErrors += row.Count
		}
		if row.Event == "close" {
			overview.Totals.FlowCloseEvents += row.Count
		}
	}
	overview.Users = sortTrafficUsers(userAgg, filter.Limit)
	overview.FlowEvents = sortTrafficEvents(eventAgg, filter.Limit)
	overview.PolicyEvents = sortPolicyEvents(policyAgg, filter.Limit)
	sort.Slice(overview.Nodes, func(i, j int) bool {
		return overview.Nodes[i].Traffic.TotalBytes > overview.Nodes[j].Traffic.TotalBytes
	})
	if len(overview.Nodes) > filter.Limit {
		overview.Nodes = overview.Nodes[:filter.Limit]
	}
	overview.Recommendations = trafficRecommendations(overview)
	return overview
}

type trafficUserAgg struct {
	row     TrafficUserStats
	nodeIDs map[string]struct{}
}

func metricTrafficDelta(first metrics.Snapshot, latest metrics.Snapshot, hasBaseline bool) TrafficByteBreakdown {
	breakdown := TrafficByteBreakdown{
		TCPClientToTarget: counterDelta(first.TCPClientToTarget, latest.TCPClientToTarget, hasBaseline),
		TCPTargetToClient: counterDelta(first.TCPTargetToClient, latest.TCPTargetToClient, hasBaseline),
		UDPClientToTarget: counterDelta(first.UDPClientToTarget, latest.UDPClientToTarget, hasBaseline),
		UDPTargetToClient: counterDelta(first.UDPTargetToClient, latest.UDPTargetToClient, hasBaseline),
	}
	breakdown.TotalBytes = breakdown.TCPClientToTarget + breakdown.TCPTargetToClient + breakdown.UDPClientToTarget + breakdown.UDPTargetToClient
	return breakdown
}

func userTrafficDelta(first metrics.UserSnapshot, latest metrics.UserSnapshot, hasBaseline bool) TrafficByteBreakdown {
	breakdown := TrafficByteBreakdown{
		TCPClientToTarget: counterDelta(first.TCPClientToTarget, latest.TCPClientToTarget, hasBaseline),
		TCPTargetToClient: counterDelta(first.TCPTargetToClient, latest.TCPTargetToClient, hasBaseline),
		UDPClientToTarget: counterDelta(first.UDPClientToTarget, latest.UDPClientToTarget, hasBaseline),
		UDPTargetToClient: counterDelta(first.UDPTargetToClient, latest.UDPTargetToClient, hasBaseline),
	}
	breakdown.TotalBytes = breakdown.TCPClientToTarget + breakdown.TCPTargetToClient + breakdown.UDPClientToTarget + breakdown.UDPTargetToClient
	return breakdown
}

func counterDelta(first int64, latest int64, hasBaseline bool) int64 {
	if !hasBaseline {
		if latest < 0 {
			return 0
		}
		return latest
	}
	if latest >= first {
		return latest - first
	}
	if latest < 0 {
		return 0
	}
	return latest
}

func addTrafficTotals(totals *TrafficTotals, traffic TrafficByteBreakdown) {
	totals.TCPClientToTarget += traffic.TCPClientToTarget
	totals.TCPTargetToClient += traffic.TCPTargetToClient
	totals.UDPClientToTarget += traffic.UDPClientToTarget
	totals.UDPTargetToClient += traffic.UDPTargetToClient
	totals.TotalBytes += traffic.TotalBytes
}

func aggregateUsers(agg map[string]*trafficUserAgg, nodeID string, first metrics.Snapshot, latest metrics.Snapshot, hasBaseline bool) {
	firstUsers := make(map[string]metrics.UserSnapshot, len(first.Users))
	for _, user := range first.Users {
		firstUsers[user.UserID] = user
	}
	for _, latestUser := range latest.Users {
		userID := strings.TrimSpace(latestUser.UserID)
		if userID == "" {
			userID = "-"
		}
		item := agg[userID]
		if item == nil {
			item = &trafficUserAgg{row: TrafficUserStats{UserID: userID}, nodeIDs: map[string]struct{}{}}
			agg[userID] = item
		}
		item.nodeIDs[nodeID] = struct{}{}
		firstUser := firstUsers[latestUser.UserID]
		breakdown := userTrafficDelta(firstUser, latestUser, hasBaseline)
		item.row.ActiveConnections += latestUser.ActiveConnections
		addUserTraffic(&item.row.Traffic, breakdown)
	}
}

func addUserTraffic(target *TrafficByteBreakdown, next TrafficByteBreakdown) {
	target.TCPClientToTarget += next.TCPClientToTarget
	target.TCPTargetToClient += next.TCPTargetToClient
	target.UDPClientToTarget += next.UDPClientToTarget
	target.UDPTargetToClient += next.UDPTargetToClient
	target.TotalBytes += next.TotalBytes
}

func aggregateFlowEvents(events map[string]*TrafficFlowEventStats, policies map[string]*TrafficPolicyEventStats, first metrics.Snapshot, latest metrics.Snapshot, hasBaseline bool) {
	firstEvents := make(map[string]metrics.FlowEventSnapshot, len(first.FlowEvents))
	for _, event := range first.FlowEvents {
		firstEvents[flowEventKey(event.Network, event.Event, event.Reason, event.GameID, event.PolicyID)] = event
	}
	for _, latestEvent := range latest.FlowEvents {
		key := flowEventKey(latestEvent.Network, latestEvent.Event, latestEvent.Reason, latestEvent.GameID, latestEvent.PolicyID)
		delta := counterDelta(firstEvents[key].Count, latestEvent.Count, hasBaseline)
		if delta <= 0 {
			continue
		}
		eventRow := events[key]
		if eventRow == nil {
			eventRow = &TrafficFlowEventStats{
				Network:  latestEvent.Network,
				Event:    latestEvent.Event,
				Reason:   latestEvent.Reason,
				GameID:   latestEvent.GameID,
				PolicyID: latestEvent.PolicyID,
			}
			events[key] = eventRow
		}
		eventRow.Count += delta
		policyKey := flowEventKey(latestEvent.Network, "", "", latestEvent.GameID, latestEvent.PolicyID)
		policyRow := policies[policyKey]
		if policyRow == nil {
			policyRow = &TrafficPolicyEventStats{
				GameID:   latestEvent.GameID,
				PolicyID: latestEvent.PolicyID,
				Network:  latestEvent.Network,
			}
			policies[policyKey] = policyRow
		}
		switch latestEvent.Event {
		case "open":
			if latestEvent.Reason == "success" {
				policyRow.Open += delta
			} else {
				policyRow.Error += delta
			}
		case "close":
			policyRow.Close += delta
		default:
			policyRow.Error += delta
		}
		policyRow.Total += delta
	}
}

func sortTrafficUsers(agg map[string]*trafficUserAgg, limit int) []TrafficUserStats {
	rows := make([]TrafficUserStats, 0, len(agg))
	for _, item := range agg {
		item.row.NodeCount = len(item.nodeIDs)
		rows = append(rows, item.row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Traffic.TotalBytes == rows[j].Traffic.TotalBytes {
			return rows[i].UserID < rows[j].UserID
		}
		return rows[i].Traffic.TotalBytes > rows[j].Traffic.TotalBytes
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}

func sortTrafficEvents(agg map[string]*TrafficFlowEventStats, limit int) []TrafficFlowEventStats {
	rows := make([]TrafficFlowEventStats, 0, len(agg))
	for _, row := range agg {
		rows = append(rows, *row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count == rows[j].Count {
			return flowEventKey(rows[i].Network, rows[i].Event, rows[i].Reason, rows[i].GameID, rows[i].PolicyID) <
				flowEventKey(rows[j].Network, rows[j].Event, rows[j].Reason, rows[j].GameID, rows[j].PolicyID)
		}
		return rows[i].Count > rows[j].Count
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}

func sortPolicyEvents(agg map[string]*TrafficPolicyEventStats, limit int) []TrafficPolicyEventStats {
	rows := make([]TrafficPolicyEventStats, 0, len(agg))
	for _, row := range agg {
		rows = append(rows, *row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Total == rows[j].Total {
			return flowEventKey(rows[i].Network, "", "", rows[i].GameID, rows[i].PolicyID) <
				flowEventKey(rows[j].Network, "", "", rows[j].GameID, rows[j].PolicyID)
		}
		return rows[i].Total > rows[j].Total
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}

func trafficRecommendations(overview TrafficOverview) []string {
	recommendations := make([]string, 0)
	if overview.Totals.ReportNodeCount == 0 {
		recommendations = append(recommendations, "还没有收到节点上报，先检查节点 panel.report_url 和 API Key。")
	}
	if overview.Totals.PolicyDriftNodes > 0 {
		recommendations = append(recommendations, "存在策略未同步节点，请在策略页面下发或检查节点命令拉取日志。")
	}
	if overview.Totals.FlowOpenErrors > 0 {
		recommendations = append(recommendations, "存在 flow 打开失败，请查看事件排行里的 network、reason、game_id 和 policy_id。")
	}
	if overview.Totals.ActiveQUICConnections == 0 && overview.Totals.ReportNodeCount > 0 {
		recommendations = append(recommendations, "当前没有活跃客户端连接，如客户端反馈断连，请同步查看节点 journalctl 日志。")
	}
	return recommendations
}

func flowEventKey(network string, event string, reason string, gameID string, policyID string) string {
	return strings.Join([]string{network, event, reason, gameID, policyID}, "\x00")
}

func policyState(current string, desired string) string {
	current = strings.TrimSpace(current)
	desired = strings.TrimSpace(desired)
	if desired == "" {
		return "not_set"
	}
	if current == "" {
		return "waiting_report"
	}
	if current == desired {
		return "synced"
	}
	return "pending"
}

func endpointLabel(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if port == 0 {
		return host
	}
	return host + ":" + strconv.Itoa(port)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
