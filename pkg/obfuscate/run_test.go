package obfuscate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunObfuscate_validation(t *testing.T) {
	tests := []struct {
		name       string
		inputPath  string
		outputPath string
		configPath string
	}{
		{
			name:       "missing input",
			inputPath:  "",
			outputPath: t.TempDir(),
		},
		{
			name:       "missing output",
			inputPath:  t.TempDir(),
			outputPath: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := RunObfuscate(tt.inputPath, tt.outputPath, tt.configPath); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestRunObfuscate_defaultConfigAndWorkers(t *testing.T) {
	inputDir := t.TempDir()
	outputDir := t.TempDir()

	var gotConfig, gotInput, gotOutput, gotReporting string
	var gotDelete bool
	var gotWorkers int

	original := cliRunner
	cliRunner = func(configPath, inputPath, outputPath string, deleteOutputFolder bool, reportingFolder string, workerCount int) error {
		gotConfig = configPath
		gotInput = inputPath
		gotOutput = outputPath
		gotDelete = deleteOutputFolder
		gotReporting = reportingFolder
		gotWorkers = workerCount
		return nil
	}
	t.Cleanup(func() { cliRunner = original })

	if err := RunObfuscate(inputDir, outputDir, ""); err != nil {
		t.Fatalf("RunObfuscate: %v", err)
	}

	if gotConfig != DefaultObfuscateConfigPath {
		t.Fatalf("config path: got %q, want %q", gotConfig, DefaultObfuscateConfigPath)
	}
	if gotInput != inputDir {
		t.Fatalf("input path: got %q, want %q", gotInput, inputDir)
	}
	if gotOutput != outputDir {
		t.Fatalf("output path: got %q, want %q", gotOutput, outputDir)
	}
	if gotReporting != outputDir {
		t.Fatalf("reporting folder: got %q, want %q", gotReporting, outputDir)
	}
	if gotDelete {
		t.Fatal("expected deleteOutputFolder=false")
	}
	if gotWorkers != DefaultWorkerCount {
		t.Fatalf("worker count: got %d, want %d", gotWorkers, DefaultWorkerCount)
	}

	logPath := filepath.Join(outputDir, ObfuscationLogFileName)
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("expected obfuscation log at %q: %v", logPath, err)
	}
}

func TestRunObfuscate_wrapsCLIError(t *testing.T) {
	original := cliRunner
	cliRunner = func(string, string, string, bool, string, int) error {
		return os.ErrNotExist
	}
	t.Cleanup(func() { cliRunner = original })

	err := RunObfuscate(t.TempDir(), t.TempDir(), "/no/such/config.yaml")
	if err == nil {
		t.Fatal("expected error")
	}
	if got, want := err.Error(), "running obfuscation"; !strings.Contains(got, want) {
		t.Fatalf("error %q should mention %q", got, want)
	}
}
