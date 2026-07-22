package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openshift/must-gather-operator/controllers/mustgather"
)

func TestBuiltBinary_obfuscateHelp(t *testing.T) {
	bin := builtBinaryPath(t)
	cmd := exec.Command(bin, "obfuscate", "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("obfuscate --help: %v\n%s", err, out)
	}
	for _, flag := range []string{"--input", "--output", "--config"} {
		if !strings.Contains(string(out), flag) {
			t.Fatalf("help missing %q:\n%s", flag, out)
		}
	}
}

func TestBuiltBinary_obfuscateMissingRequiredFlags(t *testing.T) {
	bin := builtBinaryPath(t)
	cmd := exec.Command(bin, "obfuscate")
	if err := cmd.Run(); err == nil {
		t.Fatal("expected non-zero exit when required flags missing")
	} else {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("expected exit error, got %v", err)
		}
	}
}

func TestBuiltBinary_obfuscateDoesNotRequireOperatorEnv(t *testing.T) {
	bin := builtBinaryPath(t)
	cmd := exec.Command(bin, "obfuscate", "--help")
	cmd.Env = envWithout(mustgather.DefaultMustGatherImageEnv)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("obfuscate --help without %s: %v\n%s", mustgather.DefaultMustGatherImageEnv, err, out)
	}
}

func TestBuiltBinary_managerRequiresDefaultMustGatherImage(t *testing.T) {
	bin := builtBinaryPath(t)
	cmd := exec.Command(bin, "--health-probe-bind-address=:18082")
	cmd.Env = append(envWithout(mustgather.DefaultMustGatherImageEnv), ForceRunModeEnv+"="+LocalRunMode)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected manager startup failure without DEFAULT_MUST_GATHER_IMAGE")
	}
	if !strings.Contains(string(out), mustgather.DefaultMustGatherImageEnv) {
		t.Fatalf("expected error mentioning %s, got:\n%s", mustgather.DefaultMustGatherImageEnv, out)
	}
}

func builtBinaryPath(t *testing.T) string {
	t.Helper()
	if override := os.Getenv("MUST_GATHER_OPERATOR_BIN"); override != "" {
		if _, err := os.Stat(override); err == nil {
			return override
		}
	}
	path := filepath.Join("build", "_output", "bin", "must-gather-operator")
	if _, err := os.Stat(path); err != nil {
		t.Skip("built binary not found; run make go-build before this test")
	}
	return path
}

func envWithout(key string) []string {
	var env []string
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, key+"=") {
			continue
		}
		env = append(env, entry)
	}
	return env
}
