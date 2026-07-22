package v1alpha1

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestObfuscateConfigGodocDocumentsFailureTaxonomy(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("mustgather_types.go"))
	if err != nil {
		t.Fatalf("read mustgather_types.go: %v", err)
	}

	source := string(content)

	required := []string{
		"FR-012",
		"FR-013",
		"ObfuscationConfigInvalid",
		"ObfuscationFailed",
		"ReconcileError",
		"read-only",
		"upload staging volume",
		"directoryName",
	}

	for _, phrase := range required {
		if !strings.Contains(source, phrase) {
			t.Fatalf("ObfuscateConfig godoc missing required phrase %q", phrase)
		}
	}
}
