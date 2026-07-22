package obfuscate

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	mgcleancli "github.com/openshift/must-gather-clean/pkg/cli"
)

func TestRunObfuscate_withFixture(t *testing.T) {
	inputDir := filepath.Join("testdata", "input")
	configPath := filepath.Join("testdata", "config.yaml")
	outputDir := t.TempDir()

	inputSnapshot, err := snapshotDir(inputDir)
	if err != nil {
		t.Fatalf("snapshot input: %v", err)
	}

	original := cliRunner
	cliRunner = mgcleancli.Run
	t.Cleanup(func() { cliRunner = original })

	if err := RunObfuscate(inputDir, outputDir, configPath); err != nil {
		t.Fatalf("RunObfuscate: %v", err)
	}

	afterSnapshot, err := snapshotDir(inputDir)
	if err != nil {
		t.Fatalf("snapshot input after run: %v", err)
	}
	if !snapshotsEqual(inputSnapshot, afterSnapshot) {
		t.Fatal("input directory was modified in place")
	}

	for _, name := range []string{ReportFileName, ObfuscationLogFileName} {
		path := filepath.Join(outputDir, name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %q in output: %v", name, err)
		}
	}

	secretOut := filepath.Join(outputDir, "resources", "secret.yaml")
	if _, err := os.Stat(secretOut); !os.IsNotExist(err) {
		t.Fatalf("expected Secret resource to be omitted from output, stat err=%v", err)
	}

	node1Out, err := os.ReadFile(filepath.Join(outputDir, "cluster", "node1.log"))
	if err != nil {
		t.Fatalf("read node1 output: %v", err)
	}
	node2Out, err := os.ReadFile(filepath.Join(outputDir, "cluster", "node2.log"))
	if err != nil {
		t.Fatalf("read node2 output: %v", err)
	}

	plainIP := "10.0.1.5"
	if strings.Contains(string(node1Out), plainIP) || strings.Contains(string(node2Out), plainIP) {
		t.Fatalf("output still contains cleartext IP %q\nnode1=%q\nnode2=%q", plainIP, node1Out, node2Out)
	}

	ipTokenPattern := regexp.MustCompile(`x-ipv4-\d+-x`)
	node1Token := ipTokenPattern.FindString(string(node1Out))
	node2Token := ipTokenPattern.FindString(string(node2Out))
	if node1Token == "" || node2Token == "" {
		t.Fatalf("expected obfuscated IP tokens in output\nnode1=%q\nnode2=%q", node1Out, node2Out)
	}
	if node1Token != node2Token {
		t.Fatalf("inconsistent IP token replacement: node1=%q node2=%q", node1Token, node2Token)
	}

	plainMAC := "0e:a0:e7:92:3a:a3"
	if strings.Contains(string(node1Out), plainMAC) {
		t.Fatalf("node1 output still contains cleartext MAC %q: %q", plainMAC, node1Out)
	}
}

func TestRunObfuscate_configNotFound(t *testing.T) {
	inputDir := filepath.Join("testdata", "input")
	outputDir := t.TempDir()
	missingConfig := filepath.Join("testdata", "does-not-exist-config.yaml")

	original := cliRunner
	cliRunner = mgcleancli.Run
	t.Cleanup(func() { cliRunner = original })

	err := RunObfuscate(inputDir, outputDir, missingConfig)
	if err == nil {
		t.Fatal("expected error for missing config")
	}
	if !strings.Contains(err.Error(), "running obfuscation") {
		t.Fatalf("expected wrapped obfuscation error, got: %v", err)
	}
}

func snapshotDir(root string) (map[string][]byte, error) {
	files := make(map[string][]byte)
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[rel] = content
		return nil
	})
	return files, err
}

func snapshotsEqual(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok || string(va) != string(vb) {
			return false
		}
	}
	return true
}
