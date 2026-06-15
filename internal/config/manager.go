package config

import "sync/atomic"

type Manager struct {
	path  string
	value atomic.Value
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
	cfg, err := Load(m.path)
	if err != nil {
		return nil, err
	}
	m.value.Store(cfg)
	return cfg, nil
}
