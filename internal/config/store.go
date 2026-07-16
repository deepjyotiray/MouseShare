package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"mouseshare/internal/domain"
)

type Settings struct {
	DeviceName      string              `json:"deviceName"`
	TrustedPeers    map[string]string   `json:"trustedPeers"`
	Layout          []domain.LayoutNode `json:"layout"`
	AutoAcceptFiles bool                `json:"autoAcceptFiles"`
	DownloadsDir    string              `json:"downloadsDir"`
}

type Store struct {
	path string
	mu   sync.Mutex
}

func NewStore(baseDir string) (*Store, error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, err
	}
	return &Store{path: filepath.Join(baseDir, "config.json")}, nil
}

func (s *Store) Load() (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var cfg Settings
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		cfg.TrustedPeers = map[string]string{}
		return cfg, nil
	}
	if err != nil {
		return Settings{}, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Settings{}, err
	}
	if cfg.TrustedPeers == nil {
		cfg.TrustedPeers = map[string]string{}
	}
	return cfg, nil
}

func (s *Store) Save(cfg Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if cfg.TrustedPeers == nil {
		cfg.TrustedPeers = map[string]string{}
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
