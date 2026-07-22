package obfuscate

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	mgcleancli "github.com/openshift/must-gather-clean/pkg/cli"
	"k8s.io/klog/v2"
)

type cliRunFunc func(configPath, inputPath, outputPath string, deleteOutputFolder bool, reportingFolder string, workerCount int) error

// cliRunner invokes must-gather-clean; replaced in unit tests.
var cliRunner cliRunFunc = mgcleancli.Run

// RunObfuscate obfuscates a must-gather directory using must-gather-clean.
//
// inputPath is the read-only gather root. outputPath receives the cleaned tree.
// configPath selects the obfuscation policy; when empty, DefaultObfuscateConfigPath is used.
//
// must-gather-clean writes ReportFileName (report.yaml) into outputPath. This wrapper
// additionally tees klog output to ObfuscationLogFileName in outputPath for auditability.
// Errors are returned to the caller; the upload container owns process exit codes.
func RunObfuscate(inputPath, outputPath, configPath string) error {
	if inputPath == "" {
		return fmt.Errorf("input path is required")
	}
	if outputPath == "" {
		return fmt.Errorf("output path is required")
	}
	if configPath == "" {
		configPath = DefaultObfuscateConfigPath
	}

	logBuffer, err := os.CreateTemp("", "obfuscation-log-*.txt")
	if err != nil {
		return fmt.Errorf("creating obfuscation log buffer: %w", err)
	}
	defer os.Remove(logBuffer.Name())
	defer logBuffer.Close()

	klog.SetOutput(io.MultiWriter(os.Stderr, logBuffer))
	defer klog.SetOutput(os.Stderr)

	if err := cliRunner(configPath, inputPath, outputPath, false, outputPath, DefaultWorkerCount); err != nil {
		return fmt.Errorf("running obfuscation: %w", err)
	}

	if err := persistObfuscationLog(outputPath, logBuffer); err != nil {
		return fmt.Errorf("writing obfuscation log: %w", err)
	}

	return nil
}

func persistObfuscationLog(outputPath string, logBuffer *os.File) error {
	if _, err := os.Stat(outputPath); err != nil {
		return fmt.Errorf("output directory %q missing after obfuscation: %w", outputPath, err)
	}

	logPath := filepath.Join(outputPath, ObfuscationLogFileName)
	dest, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("creating obfuscation log %q: %w", logPath, err)
	}
	defer dest.Close()

	if _, err := logBuffer.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewinding obfuscation log buffer: %w", err)
	}
	if _, err := io.Copy(dest, logBuffer); err != nil {
		return fmt.Errorf("copying obfuscation log to %q: %w", logPath, err)
	}

	return nil
}
