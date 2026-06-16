package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gaccel-node/internal/config"
)

func TestStageDownloadsVerifiesAndWritesManifest(t *testing.T) {
	payload := []byte("release package")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	stager := NewStager()
	stager.client = server.Client()
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	stager.now = func() time.Time { return now }

	stageDir := t.TempDir()
	result, err := stager.Stage(context.Background(), config.UpgradeConfig{
		StageDir:        stageDir,
		MaxPackageBytes: 1024,
		Timeout:         time.Second,
	}, Request{
		Version: "0.3.3",
		URL:     server.URL + "/gaccel-node_0.3.3_linux-amd64.tar.gz",
		SHA256:  testSHA256(payload),
	})
	if err != nil {
		t.Fatalf("Stage returned error: %v", err)
	}
	if result.SHA256 != testSHA256(payload) {
		t.Fatalf("SHA256 = %q, want %q", result.SHA256, testSHA256(payload))
	}
	if result.SizeBytes != int64(len(payload)) {
		t.Fatalf("SizeBytes = %d, want %d", result.SizeBytes, len(payload))
	}
	if _, err := os.Stat(result.FilePath); err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(result.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(manifest), `"version": "0.3.3"`) {
		t.Fatalf("manifest missing version: %s", manifest)
	}
	if filepath.Base(result.FilePath) != "gaccel-node_0.3.3_linux-amd64.tar.gz" {
		t.Fatalf("file name = %q", filepath.Base(result.FilePath))
	}
}

func TestStageRejectsHashMismatch(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("release package"))
	}))
	defer server.Close()

	stager := NewStager()
	stager.client = server.Client()

	_, err := stager.Stage(context.Background(), config.UpgradeConfig{
		StageDir:        t.TempDir(),
		MaxPackageBytes: 1024,
		Timeout:         time.Second,
	}, Request{
		Version: "0.3.3",
		URL:     server.URL + "/pkg.tar.gz",
		SHA256:  testSHA256([]byte("other")),
	})
	if err == nil {
		t.Fatal("Stage returned nil for hash mismatch")
	}
}

func TestStageRejectsHTTPByDefault(t *testing.T) {
	stager := NewStager()
	_, err := stager.Stage(context.Background(), config.UpgradeConfig{
		StageDir:        t.TempDir(),
		MaxPackageBytes: 1024,
		Timeout:         time.Second,
	}, Request{
		Version: "0.3.3",
		URL:     "http://example.com/pkg.tar.gz",
		SHA256:  testSHA256([]byte("pkg")),
	})
	if err == nil {
		t.Fatal("Stage returned nil for http URL")
	}
}

func TestStageRejectsUnsafeVersion(t *testing.T) {
	payload := []byte("release package")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	stager := NewStager()
	stager.client = server.Client()
	_, err := stager.Stage(context.Background(), config.UpgradeConfig{
		StageDir:        t.TempDir(),
		MaxPackageBytes: 1024,
		Timeout:         time.Second,
	}, Request{
		Version: "../0.3.3",
		URL:     server.URL + "/pkg.tar.gz",
		SHA256:  testSHA256(payload),
	})
	if err == nil {
		t.Fatal("Stage returned nil for unsafe version")
	}
}

func testSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
