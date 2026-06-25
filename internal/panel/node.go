package panel

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"
)

var nodeIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,95}$`)

type Node struct {
	ID                    uint64            `json:"id"`
	NodeID                string            `json:"node_id"`
	Name                  string            `json:"name"`
	Region                string            `json:"region"`
	Country               string            `json:"country"`
	Provider              string            `json:"provider"`
	LineType              string            `json:"line_type"`
	EndpointHost          string            `json:"endpoint_host"`
	EndpointPort          int               `json:"endpoint_port"`
	ALPN                  string            `json:"alpn"`
	AdminHost             string            `json:"admin_host"`
	AdminPort             int               `json:"admin_port"`
	SSHHost               string            `json:"ssh_host"`
	SSHPort               int               `json:"ssh_port"`
	SSHUser               string            `json:"ssh_user"`
	AllowTCP              bool              `json:"allow_tcp"`
	AllowUDP              bool              `json:"allow_udp"`
	HMACSecretConfigured  bool              `json:"hmac_secret_configured"`
	HMACSecretSource      string            `json:"hmac_secret_source,omitempty"`
	HMACSecretUpdatedAt   *time.Time        `json:"hmac_secret_updated_at,omitempty"`
	HMACSecretEncrypted   string            `json:"-"`
	Tags                  []string          `json:"tags"`
	Labels                map[string]string `json:"labels"`
	Status                string            `json:"status"`
	CurrentVersion        string            `json:"current_version"`
	DesiredVersion        string            `json:"desired_version"`
	CurrentPolicyRevision string            `json:"current_policy_revision"`
	DesiredPolicyRevision string            `json:"desired_policy_revision"`
	LastReportAt          *time.Time        `json:"last_report_at,omitempty"`
	LastError             string            `json:"last_error"`
	CreatedAt             time.Time         `json:"created_at"`
	UpdatedAt             time.Time         `json:"updated_at"`
}

type NodeInput struct {
	NodeID                string            `json:"node_id"`
	Name                  string            `json:"name"`
	Region                string            `json:"region"`
	Country               string            `json:"country"`
	Provider              string            `json:"provider"`
	LineType              string            `json:"line_type"`
	EndpointHost          string            `json:"endpoint_host"`
	EndpointPort          int               `json:"endpoint_port"`
	ALPN                  string            `json:"alpn"`
	AdminHost             string            `json:"admin_host"`
	AdminPort             int               `json:"admin_port"`
	SSHHost               string            `json:"ssh_host"`
	SSHPort               int               `json:"ssh_port"`
	SSHUser               string            `json:"ssh_user"`
	AllowTCP              *bool             `json:"allow_tcp"`
	AllowUDP              *bool             `json:"allow_udp"`
	HMACSecret            string            `json:"hmac_secret,omitempty"`
	Tags                  []string          `json:"tags"`
	Labels                map[string]string `json:"labels"`
	Status                string            `json:"status"`
	DesiredVersion        string            `json:"desired_version"`
	DesiredPolicyRevision string            `json:"desired_policy_revision"`
}

func NewNodeFromInput(input NodeInput) (Node, error) {
	node := Node{
		NodeID:                strings.TrimSpace(input.NodeID),
		Name:                  strings.TrimSpace(input.Name),
		Region:                strings.TrimSpace(input.Region),
		Country:               strings.TrimSpace(input.Country),
		Provider:              strings.TrimSpace(input.Provider),
		LineType:              strings.TrimSpace(input.LineType),
		EndpointHost:          strings.TrimSpace(input.EndpointHost),
		EndpointPort:          input.EndpointPort,
		ALPN:                  strings.TrimSpace(input.ALPN),
		AdminHost:             strings.TrimSpace(input.AdminHost),
		AdminPort:             input.AdminPort,
		SSHHost:               strings.TrimSpace(input.SSHHost),
		SSHPort:               input.SSHPort,
		SSHUser:               strings.TrimSpace(input.SSHUser),
		AllowTCP:              true,
		AllowUDP:              true,
		Tags:                  cleanList(input.Tags),
		Labels:                cleanLabels(input.Labels),
		Status:                strings.TrimSpace(input.Status),
		DesiredVersion:        strings.TrimSpace(input.DesiredVersion),
		DesiredPolicyRevision: strings.TrimSpace(input.DesiredPolicyRevision),
	}
	if input.AllowTCP != nil {
		node.AllowTCP = *input.AllowTCP
	}
	if input.AllowUDP != nil {
		node.AllowUDP = *input.AllowUDP
	}
	if node.ALPN == "" {
		node.ALPN = "gaccel/1"
	}
	if node.AdminHost == "" {
		node.AdminHost = "127.0.0.1"
	}
	if node.AdminPort == 0 {
		node.AdminPort = 5557
	}
	if node.SSHHost == "" {
		node.SSHHost = node.EndpointHost
	}
	if node.SSHPort == 0 {
		node.SSHPort = 22
	}
	if node.SSHUser == "" {
		node.SSHUser = "root"
	}
	if node.Status == "" {
		node.Status = "new"
	}
	if err := ValidateNode(node); err != nil {
		return Node{}, err
	}
	return node, nil
}

func ValidateNode(node Node) error {
	if !nodeIDPattern.MatchString(node.NodeID) {
		return errors.New("node_id must be 1-96 chars and only contain letters, numbers, '_', '.', ':', '-'")
	}
	if node.Name == "" {
		return errors.New("name is required")
	}
	if err := validateHost("endpoint_host", node.EndpointHost); err != nil {
		return err
	}
	if err := validatePort("endpoint_port", node.EndpointPort); err != nil {
		return err
	}
	if node.ALPN == "" {
		return errors.New("alpn is required")
	}
	if err := validateHost("admin_host", node.AdminHost); err != nil {
		return err
	}
	if err := validatePort("admin_port", node.AdminPort); err != nil {
		return err
	}
	if err := validateHost("ssh_host", node.SSHHost); err != nil {
		return err
	}
	if err := validatePort("ssh_port", node.SSHPort); err != nil {
		return err
	}
	if node.SSHUser == "" {
		return errors.New("ssh_user is required")
	}
	if !isAllowedNodeStatus(node.Status) {
		return fmt.Errorf("status %q is not allowed", node.Status)
	}
	return nil
}

func validateHost(field string, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if strings.ContainsAny(value, "/\\") {
		return fmt.Errorf("%s must be a host or IP, not a URL/path", field)
	}
	if ip := net.ParseIP(value); ip != nil {
		return nil
	}
	if len(value) > 255 {
		return fmt.Errorf("%s is too long", field)
	}
	return nil
}

func validatePort(field string, value int) error {
	if value <= 0 || value > 65535 {
		return fmt.Errorf("%s must be between 1 and 65535", field)
	}
	return nil
}

func isAllowedNodeStatus(status string) bool {
	switch status {
	case "new", "deploying", "online", "offline", "error", "disabled":
		return true
	default:
		return false
	}
}

func cleanLabels(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	cleaned := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			continue
		}
		cleaned[key] = value
	}
	return cleaned
}
