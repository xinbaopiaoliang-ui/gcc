package panel

import (
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	PolicySourceBackend = "backend"
	PolicySourceManual  = "manual"
)

var policyRevisionPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,63}$`)

type PolicyRevisionInput struct {
	Revision          string `json:"revision"`
	SHA256            string `json:"sha256,omitempty"`
	RoutePoliciesYAML string `json:"route_policies_yaml"`
	Source            string `json:"source,omitempty"`
}

type PolicyRevision struct {
	ID                uint64    `json:"id"`
	Revision          string    `json:"revision"`
	SHA256            string    `json:"sha256"`
	RoutePoliciesYAML string    `json:"route_policies_yaml"`
	Source            string    `json:"source"`
	CreatedAt         time.Time `json:"created_at"`
}

type NodePolicyRevision struct {
	ID        uint64     `json:"id"`
	NodeID    string     `json:"node_id"`
	Revision  string     `json:"revision"`
	Desired   bool       `json:"desired"`
	Applied   bool       `json:"applied"`
	AppliedAt *time.Time `json:"applied_at,omitempty"`
	LastError string     `json:"last_error"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func NewPolicyRevisionFromInput(input PolicyRevisionInput) (PolicyRevision, error) {
	revision := strings.TrimSpace(input.Revision)
	if !policyRevisionPattern.MatchString(revision) {
		return PolicyRevision{}, errors.New("revision must be 1-64 chars and only contain letters, numbers, '_', '.', ':', '-'")
	}
	if strings.TrimSpace(input.RoutePoliciesYAML) == "" {
		return PolicyRevision{}, errors.New("route_policies_yaml is required")
	}
	validation := ValidatePolicyPackage(PolicyValidationRequest{
		Revision:          revision,
		SHA256:            input.SHA256,
		RoutePoliciesYAML: input.RoutePoliciesYAML,
	})
	if !validation.Valid {
		return PolicyRevision{}, errors.New(strings.Join(validation.Errors, "; "))
	}
	source := strings.TrimSpace(input.Source)
	if source == "" {
		source = PolicySourceBackend
	}
	if source != PolicySourceBackend && source != PolicySourceManual {
		return PolicyRevision{}, fmt.Errorf("source %q is not allowed", source)
	}
	computedSHA := sha256Hex([]byte(input.RoutePoliciesYAML))
	shaValue := normalizeSHA256(input.SHA256)
	if shaValue == "" {
		shaValue = computedSHA
	}
	if !isSHA256Hex(shaValue) {
		return PolicyRevision{}, errors.New("sha256 must be 64 lowercase hex chars")
	}
	if shaValue != computedSHA {
		return PolicyRevision{}, errors.New("sha256 does not match route_policies_yaml")
	}
	return PolicyRevision{
		Revision:          revision,
		SHA256:            shaValue,
		RoutePoliciesYAML: input.RoutePoliciesYAML,
		Source:            source,
	}, nil
}

func validatePolicyRevisionID(revision string) error {
	if !policyRevisionPattern.MatchString(strings.TrimSpace(revision)) {
		return errors.New("revision is invalid")
	}
	return nil
}

func isSHA256Hex(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
