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
