//go:build e2e
// +build e2e

package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/openshift/must-gather-operator/pkg/obfuscate"
)

func TestVerifyObfuscatedBundleRootFixture(t *testing.T) {
	root := filepath.Join("testdata", "obfuscate-bundle")
	if err := VerifyObfuscatedBundleRoot(root, BundleVerifyOptions{}); err != nil {
		t.Fatalf("VerifyObfuscatedBundleRoot: %v", err)
	}
}

func TestVerifyObfuscatedBundleRootDetectsSecretResource(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "resources"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, obfuscate.ObfuscationLogFileName), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "resources", "secret.yaml"), []byte("kind: Secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyObfuscatedBundleRoot(root, BundleVerifyOptions{}); err == nil {
		t.Fatal("expected Secret resource detection to fail SC-002")
	}
}

func TestVerifyObfuscatedBundleRootDetectsCleartextIP(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "cluster"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, obfuscate.ObfuscationLogFileName), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cluster", "node.log"), []byte("ip 192.168.1.10\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyObfuscatedBundleRoot(root, BundleVerifyOptions{}); err == nil {
		t.Fatal("expected cleartext IP detection to fail SC-001")
	}
}

func TestVerifyObfuscatedBundleRootAllowsMACWhenConfigured(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "cluster"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, obfuscate.ObfuscationLogFileName), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cluster", "node.log"), []byte("mac aa:bb:cc:dd:ee:ff\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := VerifyObfuscatedBundleRoot(root, BundleVerifyOptions{AllowMACCleartext: true}); err != nil {
		t.Fatalf("expected MAC cleartext allowed for SC-004 custom policy: %v", err)
	}
}
