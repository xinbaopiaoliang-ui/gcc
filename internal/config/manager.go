package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"gopkg.in/yaml.v3"
)

type Manager struct {
	path  string
	value atomic.Value
	mu    sync.Mutex
}

type ApplyPackageResult struct {
	Config     *Config
	BackupPath string
	SHA256     string
}

func NewManager(path string) (*Manager, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}
	manager := &Manager{path: path}
	manager.value.Store(cfg)
	return manager, nil
}

func (m *Manager) Current() *Config {
	cfg, _ := m.value.Load().(*Config)
	return cfg
}

func (m *Manager) Path() string {
	return m.path
}

func (m *Manager) Reload() (*Config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reloadLocked()
}

func (m *Manager) ApplyPackage(data []byte, expectedSHA256 string) (*ApplyPackageResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("config package is empty")
	}
	actualSHA := sha256Hex(data)
	expectedSHA256 = normalizeSHA256(expectedSHA256)
	if expectedSHA256 == "" {
		return nil, errors.New("config package sha256 is required")
	}
	if actualSHA != expectedSHA256 {
		return nil, fmt.Errorf("config package sha256 mismatch: got %s", actualSHA)
	}
	if _, err := LoadData(data); err != nil {
		return nil, fmt.Errorf("config package validation failed: %w", err)
	}

	return m.applyConfigDataLocked(data, actualSHA)
}

func (m *Manager) ApplyRoutePolicies(data []byte, expectedSHA256 string) (*ApplyPackageResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("route policy package is empty")
	}
	actualSHA := sha256Hex(data)
	expectedSHA256 = normalizeSHA256(expectedSHA256)
	if expectedSHA256 == "" {
		return nil, errors.New("route policy package sha256 is required")
	}
	if actualSHA != expectedSHA256 {
		return nil, fmt.Errorf("route policy package sha256 mismatch: got %s", actualSHA)
	}

	oldData, err := os.ReadFile(m.path)
	if err != nil {
		return nil, err
	}
	merged, err := replaceRoutePoliciesYAML(oldData, data)
	if err != nil {
		return nil, err
	}
	if _, err := LoadData(merged); err != nil {
		return nil, fmt.Errorf("route policy package validation failed: %w", err)
	}

	return m.applyConfigDataLocked(merged, actualSHA)
}

func (m *Manager) applyConfigDataLocked(data []byte, actualSHA string) (*ApplyPackageResult, error) {
	oldCfg := m.Current()
	oldData, err := os.ReadFile(m.path)
	if err != nil {
		return nil, err
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(m.path); err == nil {
		mode = info.Mode().Perm()
	}

	backupPath := m.path + ".rollback"
	if err := os.WriteFile(backupPath, oldData, mode); err != nil {
		return nil, fmt.Errorf("write rollback backup: %w", err)
	}
	if err := writeFileAtomic(m.path, data, mode); err != nil {
		return nil, fmt.Errorf("write candidate config: %w", err)
	}

	cfg, err := m.reloadLocked()
	if err != nil {
		restoreErr := writeFileAtomic(m.path, oldData, mode)
		if oldCfg != nil {
			m.value.Store(oldCfg)
		}
		if restoreErr != nil {
			return nil, fmt.Errorf("reload candidate failed: %w; rollback failed: %v", err, restoreErr)
		}
		return nil, fmt.Errorf("reload candidate failed and was rolled back: %w", err)
	}
	return &ApplyPackageResult{
		Config:     cfg,
		BackupPath: backupPath,
		SHA256:     actualSHA,
	}, nil
}

func (m *Manager) reloadLocked() (*Config, error) {
	cfg, err := Load(m.path)
	if err != nil {
		return nil, err
	}
	m.value.Store(cfg)
	return cfg, nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".gaccel-config-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func normalizeSHA256(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, "sha256:")
	return value
}

func ParseRoutePoliciesData(data []byte) (RoutePoliciesConfig, error) {
	policyNode, err := extractRoutePoliciesNode(data)
	if err != nil {
		return RoutePoliciesConfig{}, err
	}
	var routePolicies RoutePoliciesConfig
	if err := policyNode.Decode(&routePolicies); err != nil {
		return RoutePoliciesConfig{}, fmt.Errorf("decode route policies: %w", err)
	}
	if err := validateRoutePolicies(routePolicies); err != nil {
		return RoutePoliciesConfig{}, err
	}
	return routePolicies, nil
}

func replaceRoutePoliciesYAML(configData, policyData []byte) ([]byte, error) {
	policyNode, err := extractRoutePoliciesNode(policyData)
	if err != nil {
		return nil, err
	}

	var root yaml.Node
	if err := yaml.Unmarshal(configData, &root); err != nil {
		return nil, fmt.Errorf("read current config yaml: %w", err)
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("current config yaml must be a mapping")
	}
	mapping := root.Content[0]
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == "route_policies" {
			mapping.Content[i+1] = policyNode
			return yaml.Marshal(&root)
		}
	}
	mapping.Content = append(
		mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: "route_policies"},
		policyNode,
	)
	return yaml.Marshal(&root)
}

func extractRoutePoliciesNode(data []byte) (*yaml.Node, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("read route policy yaml: %w", err)
	}
	if len(root.Content) == 0 {
		return nil, errors.New("route policy yaml is empty")
	}
	node := root.Content[0]
	if node.Kind != yaml.MappingNode {
		return nil, errors.New("route policy yaml must be a mapping")
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == "route_policies" {
			return node.Content[i+1], nil
		}
	}

	var routePolicies RoutePoliciesConfig
	if err := node.Decode(&routePolicies); err != nil {
		return nil, fmt.Errorf("decode route policy yaml: %w", err)
	}
	if len(routePolicies.Policies) == 0 && strings.TrimSpace(routePolicies.Revision) == "" {
		return nil, errors.New("route policy yaml must contain route_policies or revision/policies")
	}
	return node, nil
}
