package obfuscate

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestDefaultConfigPath(t *testing.T) {
	if DefaultConfigPath != "/etc/must-gather-clean/default-config.yaml" {
		t.Fatalf("unexpected default config path: %q", DefaultConfigPath)
	}
}

func TestRunRequiresInputAndOutput(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "missing both", args: nil},
		{name: "missing output", args: []string{"--input", "/tmp/in"}},
		{name: "missing input", args: []string{"--output", "/tmp/out"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Run(tc.args); got == 0 {
				t.Fatal("expected non-zero exit when required flags missing")
			}
		})
	}
}

func TestRunRejectsMissingConfigFile(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	missingConfig := filepath.Join(t.TempDir(), "missing-config.yaml")

	args := []string{
		"--input", inputDir,
		"--output", outputDir,
		"--config", missingConfig,
	}

	if got := Run(args); got == 0 {
		t.Fatal("expected non-zero exit when config file is missing")
	}
}

func TestRunWithDefaultConfig(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()

	writeTestFile(t, inputDir, "hosts-a.txt", "host 10.0.0.5\n")
	writeTestFile(t, inputDir, "hosts-b.txt", "peer 10.0.0.5\n")
	writeTestFile(t, inputDir, "secret.yaml", `apiVersion: v1
kind: Secret
metadata:
  name: test-secret
  namespace: default
data:
  token: YWJj
`)

	args := []string{
		"--input", inputDir,
		"--output", outputDir,
		"--config", defaultConfigPath(),
	}
	if got := Run(args); got != 0 {
		t.Fatalf("expected successful obfuscation with default config, exit=%d", got)
	}

	assertReportExists(t, outputDir)

	outputA := readTestFile(t, filepath.Join(outputDir, "hosts-a.txt"))
	outputB := readTestFile(t, filepath.Join(outputDir, "hosts-b.txt"))
	if strings.Contains(outputA, "10.0.0.5") || strings.Contains(outputB, "10.0.0.5") {
		t.Fatalf("expected IP obfuscation, got a=%q b=%q", outputA, outputB)
	}

	tokenA := extractConsistentIPToken(t, outputA)
	tokenB := extractConsistentIPToken(t, outputB)
	if tokenA != tokenB {
		t.Fatalf("expected consistent IP token across files, got %q and %q", tokenA, tokenB)
	}

	if _, err := os.Stat(filepath.Join(outputDir, "secret.yaml")); !os.IsNotExist(err) {
		t.Fatalf("expected Secret YAML to be omitted from output, stat err=%v", err)
	}
}

func TestRunWithCustomIPOnlyConfig(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	config := []byte(`config:
  obfuscate:
    - type: IP
      replacementType: Consistent
      target: All
  omit: []
`)
	writeTestFile(t, filepath.Dir(configPath), filepath.Base(configPath), string(config))
	writeTestFile(t, inputDir, "sample.txt", "ip 10.0.0.1 mac aa:bb:cc:dd:ee:ff\n")

	args := []string{
		"--input", inputDir,
		"--output", outputDir,
		"--config", configPath,
	}
	if got := Run(args); got != 0 {
		t.Fatalf("expected successful obfuscation with custom config, exit=%d", got)
	}

	assertReportExists(t, outputDir)

	output := readTestFile(t, filepath.Join(outputDir, "sample.txt"))
	if strings.Contains(output, "10.0.0.1") {
		t.Fatalf("expected IP obfuscation, got %q", output)
	}
	if !strings.Contains(output, "aa:bb:cc:dd:ee:ff") {
		t.Fatalf("expected MAC preserved with IP-only config, got %q", output)
	}
	extractConsistentIPToken(t, output)
}

func TestRunSmokeProducesReport(t *testing.T) {
	if os.Getenv("OBFUSCATE_SMOKE") != "1" {
		t.Skip("set OBFUSCATE_SMOKE=1 to run integration smoke test")
	}

	inputDir := t.TempDir()
	outputDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	config := []byte(`config:
  obfuscate:
    - type: IP
      replacementType: Consistent
      target: All
  omit: []
`)
	if err := os.WriteFile(configPath, config, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(inputDir, "sample.txt"), []byte("contact 10.0.0.1\n"), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}

	args := []string{"--input", inputDir, "--output", outputDir, "--config", configPath}
	if got := Run(args); got != 0 {
		t.Fatalf("expected successful obfuscation, exit=%d", got)
	}

	if _, err := os.Stat(filepath.Join(outputDir, "report.yaml")); err != nil {
		t.Fatalf("expected report.yaml in output: %v", err)
	}
}

func defaultConfigPath() string {
	return filepath.Join("..", "..", "build", "obfuscate-config.yaml")
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func assertReportExists(t *testing.T, outputDir string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(outputDir, "report.yaml")); err != nil {
		t.Fatalf("expected report.yaml in output: %v", err)
	}
}

var consistentIPTokenPattern = regexp.MustCompile(`x-ipv4-\d{10}-x`)

func extractConsistentIPToken(t *testing.T, content string) string {
	t.Helper()
	match := consistentIPTokenPattern.FindString(content)
	if match == "" {
		t.Fatalf("expected consistent IP replacement token in %q", content)
	}
	return match
}
