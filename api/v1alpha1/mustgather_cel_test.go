package v1alpha1

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func normalizeCEL(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "\n", " ")), " ")
}

func readCRD(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve test file path")
	}

	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	crdPath := filepath.Join(repoRoot, "deploy", "crds", "operator.openshift.io_mustgathers.yaml")

	content, err := os.ReadFile(crdPath)
	if err != nil {
		t.Fatalf("read CRD %s: %v", crdPath, err)
	}

	return string(content)
}

func TestObfuscateCELRulesPresentInCRD(t *testing.T) {
	crd := normalizeCEL(readCRD(t))

	expected := []struct {
		name    string
		rule    string
		message string
	}{
		{
			name:    "FR-012 enabled without source or upload",
			rule:    "!(has(self.obfuscate) && has(self.obfuscate.enabled) && self.obfuscate.enabled && !has(self.obfuscate.source) && !has(self.uploadTarget))",
			message: "obfuscate.enabled requires either obfuscate.source or uploadTarget",
		},
		{
			name:    "FR-013 source without enabled",
			rule:    "!(has(self.obfuscate) && has(self.obfuscate.source) && (!has(self.obfuscate.enabled) || !self.obfuscate.enabled))",
			message: "obfuscate.source requires obfuscate.enabled to be true",
		},
	}

	for _, tc := range expected {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(crd, normalizeCEL(tc.rule)) {
				t.Fatalf("CRD missing expected CEL rule %q", tc.rule)
			}
			if !strings.Contains(crd, tc.message) {
				t.Fatalf("CRD missing expected validation message %q", tc.message)
			}
		})
	}
}

func TestObfuscateSourceSubPathMinLengthInCRD(t *testing.T) {
	crd := readCRD(t)

	marker := "whitespace-only values are trimmed at runtime in the Job template"
	idx := strings.Index(crd, marker)
	if idx == -1 {
		t.Fatalf("CRD missing obfuscate.source.subPath documentation marker %q", marker)
	}

	snippet := crd[idx : min(len(crd), idx+250)]
	if !strings.Contains(snippet, "minLength: 1") {
		t.Fatalf("obfuscate.source.subPath missing minLength: 1 in CRD schema")
	}
}
