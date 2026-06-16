package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gaccel-node/internal/config"
)

var (
	safeFileNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	safeVersionPattern  = regexp.MustCompile(`^[A-Za-z0-9._+-]+$`)
)

type Stager struct {
	client *http.Client
	now    func() time.Time
}

type Request struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	File    string `json:"file,omitempty"`
}

type Result struct {
	Version      string    `json:"version"`
	URL          string    `json:"url"`
	FilePath     string    `json:"file_path"`
	ManifestPath string    `json:"manifest_path"`
	SHA256       string    `json:"sha256"`
	SizeBytes    int64     `json:"size_bytes"`
	StagedAt     time.Time `json:"staged_at"`
}

type Manifest struct {
	Version   string    `json:"version"`
	URL       string    `json:"url"`
	FileName  string    `json:"file_name"`
	SHA256    string    `json:"sha256"`
	SizeBytes int64     `json:"size_bytes"`
	StagedAt  time.Time `json:"staged_at"`
}

func NewStager() *Stager {
	return &Stager{
		client: &http.Client{},
		now:    time.Now,
	}
}

func (s *Stager) Stage(ctx context.Context, cfg config.UpgradeConfig, req Request) (*Result, error) {
	req.Version = strings.TrimSpace(req.Version)
	req.URL = strings.TrimSpace(req.URL)
	req.SHA256 = normalizeSHA256(req.SHA256)
	req.File = strings.TrimSpace(req.File)
	if req.Version == "" {
		return nil, errors.New("upgrade version is required")
	}
	if !safeVersion(req.Version) {
		return nil, fmt.Errorf("upgrade version is invalid: %q", req.Version)
	}
	if req.URL == "" {
		return nil, errors.New("upgrade url is required")
	}
	if req.SHA256 == "" {
		return nil, errors.New("upgrade sha256 is required")
	}
	downloadURL, err := url.Parse(req.URL)
	if err != nil {
		return nil, fmt.Errorf("upgrade url is invalid: %w", err)
	}
	if downloadURL.Scheme != "https" && !(cfg.AllowHTTP && downloadURL.Scheme == "http") {
		return nil, errors.New("upgrade url must use https")
	}
	if downloadURL.Host == "" {
		return nil, errors.New("upgrade url host is required")
	}
	fileName := req.File
	if fileName == "" {
		fileName = filepath.Base(downloadURL.Path)
	}
	if !safeFileName(fileName) {
		return nil, fmt.Errorf("upgrade file name is invalid: %q", fileName)
	}
	if cfg.MaxPackageBytes <= 0 {
		cfg.MaxPackageBytes = 200 * 1024 * 1024
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 2 * time.Minute
	}

	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "gaccel-node-upgrader")
	response, err := s.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("upgrade download returned %s", response.Status)
	}
	if response.ContentLength > cfg.MaxPackageBytes {
		return nil, fmt.Errorf("upgrade package is too large: %d bytes", response.ContentLength)
	}

	stageDir := filepath.Join(cfg.StageDir, req.Version)
	if err := os.MkdirAll(stageDir, 0o750); err != nil {
		return nil, err
	}
	filePath := filepath.Join(stageDir, fileName)
	tmp, err := os.CreateTemp(stageDir, ".download-*")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	hasher := sha256.New()
	limited := io.LimitReader(response.Body, cfg.MaxPackageBytes+1)
	n, err := io.Copy(io.MultiWriter(tmp, hasher), limited)
	if err != nil {
		_ = tmp.Close()
		return nil, err
	}
	if n > cfg.MaxPackageBytes {
		_ = tmp.Close()
		return nil, fmt.Errorf("upgrade package exceeds max size: %d bytes", n)
	}
	if err := tmp.Chmod(0o640); err != nil {
		_ = tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	actualSHA := hex.EncodeToString(hasher.Sum(nil))
	if actualSHA != req.SHA256 {
		return nil, fmt.Errorf("upgrade sha256 mismatch: got %s", actualSHA)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		return nil, err
	}

	stagedAt := s.now().UTC()
	manifest := Manifest{
		Version:   req.Version,
		URL:       downloadURL.String(),
		FileName:  fileName,
		SHA256:    actualSHA,
		SizeBytes: n,
		StagedAt:  stagedAt,
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	manifestPath := filepath.Join(stageDir, "manifest.json")
	if err := writeFileAtomic(manifestPath, append(manifestData, '\n'), 0o640); err != nil {
		_ = os.Remove(filePath)
		return nil, err
	}

	return &Result{
		Version:      req.Version,
		URL:          downloadURL.String(),
		FilePath:     filePath,
		ManifestPath: manifestPath,
		SHA256:       actualSHA,
		SizeBytes:    n,
		StagedAt:     stagedAt,
	}, nil
}

func safeFileName(name string) bool {
	if name == "." || name == "/" || name == "" {
		return false
	}
	return safeFileNamePattern.MatchString(name)
}

func safeVersion(version string) bool {
	if version == "." || version == ".." || version == "" {
		return false
	}
	return safeVersionPattern.MatchString(version)
}

func normalizeSHA256(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimPrefix(value, "sha256:")
	return value
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".manifest-*")
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
