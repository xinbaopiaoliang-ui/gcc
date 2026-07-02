package panel

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gaccel-node/internal/config"
	"gaccel-node/internal/metrics"
	"gaccel-node/internal/panelcommand"
	"gaccel-node/internal/sessions"
)

const (
	TaskTypeDeployNode  = "deploy_node"
	TaskTypeUpdateNode  = "update_node"
	TaskTypeRestartNode = "restart_node"
	TaskTypeApplyPolicy = panelcommand.CommandApplyPolicy

	TaskStatusPending   = "pending"
	TaskStatusRunning   = "running"
	TaskStatusSuccess   = "success"
	TaskStatusFailed    = "failed"
	TaskStatusCancelled = "cancelled"
)

type NodeReportPayload struct {
	Status        string                       `json:"status"`
	Version       string                       `json:"version"`
	Timestamp     time.Time                    `json:"timestamp"`
	Node          config.NodeConfig            `json:"node"`
	Server        ReportServerInfo             `json:"server"`
	RoutePolicies ReportRoutePoliciesInfo      `json:"route_policies"`
	Metrics       metrics.Snapshot             `json:"metrics"`
	Sessions      []sessions.Snapshot          `json:"sessions,omitempty"`
	SessionEvents []sessions.Event             `json:"session_events,omitempty"`
	PanelCommands []panelcommand.CommandResult `json:"panel_commands,omitempty"`
}

type ReportServerInfo struct {
	Listen string `json:"listen"`
	ALPN   string `json:"alpn"`
}

type ReportRoutePoliciesInfo struct {
	Mode        string `json:"mode"`
	Revision    string `json:"revision"`
	PolicyCount int    `json:"policy_count"`
}

type ApplyPolicyTaskRequest struct {
	Revision          string `json:"revision,omitempty"`
	RoutePoliciesYAML string `json:"route_policies_yaml"`
	SHA256            string `json:"sha256,omitempty"`
	Priority          int    `json:"priority,omitempty"`
}

func NewApplyPolicyTask(nodeID string, req ApplyPolicyTaskRequest, now time.Time) (NodeTaskInput, error) {
	nodeID = strings.TrimSpace(nodeID)
	policyYAML := strings.TrimSpace(req.RoutePoliciesYAML)
	if nodeID == "" {
		return NodeTaskInput{}, errors.New("node_id is required")
	}
	if policyYAML == "" {
		return NodeTaskInput{}, errors.New("route_policies_yaml is required")
	}
	shaValue := normalizeSHA256(req.SHA256)
	if shaValue == "" {
		shaValue = sha256Hex([]byte(req.RoutePoliciesYAML))
	}
	priority := req.Priority
	if priority <= 0 {
		priority = 100
	}
	taskID, err := newTaskID(TaskTypeApplyPolicy, now)
	if err != nil {
		return NodeTaskInput{}, err
	}
	return NodeTaskInput{
		TaskID:   taskID,
		NodeID:   nodeID,
		Type:     TaskTypeApplyPolicy,
		Status:   TaskStatusPending,
		Priority: priority,
		RequestJSON: panelcommand.ApplyPolicyPayload{
			SHA256:            shaValue,
			RoutePoliciesYAML: req.RoutePoliciesYAML,
		},
	}, nil
}

func nodeCommandFromTask(task NodeTask, issuedAt time.Time, expiresIn time.Duration) panelcommand.Command {
	if expiresIn <= 0 {
		expiresIn = 2 * time.Minute
	}
	return panelcommand.Command{
		ID:        task.TaskID,
		Type:      task.Type,
		IssuedAt:  issuedAt.UTC(),
		ExpiresAt: issuedAt.Add(expiresIn).UTC(),
		Payload:   task.RequestJSON,
	}
}

func signCommandBody(secret string, body []byte, now time.Time) (timestamp string, nonce string, signature string, err error) {
	nonce, err = randomHex(16)
	if err != nil {
		return "", "", "", err
	}
	timestamp = now.UTC().Format(time.RFC3339Nano)
	signature = panelcommand.SignBody(secret, timestamp, nonce, body)
	return timestamp, nonce, signature, nil
}

func newTaskID(prefix string, now time.Time) (string, error) {
	suffix, err := randomHex(6)
	if err != nil {
		return "", err
	}
	prefix = strings.ReplaceAll(strings.TrimSpace(prefix), "_", "-")
	if prefix == "" {
		prefix = "task"
	}
	return fmt.Sprintf("%s-%s-%s", prefix, now.UTC().Format("20060102T150405"), suffix), nil
}

func randomHex(bytesLen int) (string, error) {
	if bytesLen <= 0 {
		bytesLen = 16
	}
	data := make([]byte, bytesLen)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func normalizeSHA256(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	return strings.TrimPrefix(value, "sha256:")
}

func latestCommandError(results []panelcommand.CommandResult) string {
	for i := len(results) - 1; i >= 0; i-- {
		if !results[i].OK && strings.TrimSpace(results[i].Error) != "" {
			return strings.TrimSpace(results[i].Error)
		}
	}
	return ""
}

func jsonRaw(value any) (json.RawMessage, error) {
	if value == nil {
		return json.RawMessage("null"), nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}
