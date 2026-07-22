package obfuscate

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	mgcleancli "github.com/openshift/must-gather-clean/pkg/cli"
)

func TestRunObfuscate_withDefaultConfig(t *testing.T) {
	configPath := filepath.Join("..", "..", "build", "obfuscate-config.yaml")
	if _, err := os.Stat(configPath); err != nil {
		t.Skipf("default config not present at %q: %v", configPath, err)
	}

	inputDir := filepath.Join("testdata", "input")
	outputDir := t.TempDir()

	inputSnapshot, err := snapshotDir(inputDir)
	if err != nil {
		t.Fatalf("snapshot input: %v", err)
	}

	original := cliRunner
	cliRunner = mgcleancli.Run
	t.Cleanup(func() { cliRunner = original })

	if err := RunObfuscate(inputDir, outputDir, configPath); err != nil {
		t.Fatalf("RunObfuscate with default config: %v", err)
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

	if len(node1Out) == 0 || len(node2Out) == 0 {
		t.Fatal("expected non-empty obfuscated output files")
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
