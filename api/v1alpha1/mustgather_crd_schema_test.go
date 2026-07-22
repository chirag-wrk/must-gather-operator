package v1alpha1

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	yamlv3 "gopkg.in/yaml.v3"
	"sigs.k8s.io/yaml"
)

func repoRoot(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to resolve test file path")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func readCRDBytes(t *testing.T, relPath string) []byte {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(repoRoot(t), relPath))
	if err != nil {
		t.Fatalf("read CRD %s: %v", relPath, err)
	}

	return content
}

func mustGatherSpecSchema(t *testing.T, crdBytes []byte) map[string]any {
	t.Helper()

	var crd map[string]any
	if err := yamlv3.Unmarshal(crdBytes, &crd); err != nil {
		t.Fatalf("unmarshal CRD: %v", err)
	}

	spec, ok := crd["spec"].(map[string]any)
	if !ok {
		t.Fatal("CRD missing spec")
	}

	versions, ok := spec["versions"].([]any)
	if !ok || len(versions) == 0 {
		t.Fatal("CRD missing versions")
	}

	version, ok := versions[0].(map[string]any)
	if !ok {
		t.Fatal("CRD version entry has unexpected shape")
	}

	schema, ok := version["schema"].(map[string]any)
	if !ok {
		t.Fatal("CRD version missing schema")
	}

	openAPI, ok := schema["openAPIV3Schema"].(map[string]any)
	if !ok {
		t.Fatal("CRD schema missing openAPIV3Schema")
	}

	properties, ok := openAPI["properties"].(map[string]any)
	if !ok {
		t.Fatal("CRD openAPIV3Schema missing properties")
	}

	mgSpec, ok := properties["spec"].(map[string]any)
	if !ok {
		t.Fatal("CRD missing spec properties")
	}

	return mgSpec
}

func TestDeployAndBundleCRDSchemasMatch(t *testing.T) {
	deploy := readCRDBytes(t, filepath.Join("deploy", "crds", "operator.openshift.io_mustgathers.yaml"))
	bundle := readCRDBytes(t, filepath.Join("bundle", "manifests", "tech-preview", "operator.openshift.io_mustgathers.yaml"))

	if !bytes.Equal(deploy, bundle) {
		t.Fatal("deploy and bundle MustGather CRD copies differ; regenerate and sync in T1_3")
	}
}

func TestMustGatherSpecRequiredFieldsBackwardCompatible(t *testing.T) {
	mgSpec := mustGatherSpecSchema(t, readCRDBytes(t, filepath.Join("deploy", "crds", "operator.openshift.io_mustgathers.yaml")))

	required, ok := mgSpec["required"].([]any)
	if !ok {
		t.Fatal("MustGather spec missing required list")
	}

	if len(required) != 1 {
		t.Fatalf("expected exactly one required MustGather spec field, got %v", required)
	}

	if required[0] != "serviceAccountName" {
		t.Fatalf("expected serviceAccountName to remain the sole required field, got %v", required)
	}

	properties, ok := mgSpec["properties"].(map[string]any)
	if !ok {
		t.Fatal("MustGather spec missing properties")
	}

	if _, ok := properties["obfuscate"]; !ok {
		t.Fatal("CRD spec.properties missing optional obfuscate field")
	}
}

func TestObfuscateFieldsAreOptionalInCRD(t *testing.T) {
	mgSpec := mustGatherSpecSchema(t, readCRDBytes(t, filepath.Join("deploy", "crds", "operator.openshift.io_mustgathers.yaml")))

	properties := mgSpec["properties"].(map[string]any)
	obfuscate, ok := properties["obfuscate"].(map[string]any)
	if !ok {
		t.Fatal("obfuscate property missing from CRD")
	}

	if _, ok := obfuscate["required"]; ok {
		t.Fatal("obfuscate block must not declare required fields")
	}

	obProps, ok := obfuscate["properties"].(map[string]any)
	if !ok {
		t.Fatal("obfuscate missing properties")
	}

	for _, field := range []string{"enabled", "obfuscationConfigRef", "source"} {
		if _, ok := obProps[field]; !ok {
			t.Fatalf("obfuscate.%s missing from CRD schema", field)
		}
	}

	source, ok := obProps["source"].(map[string]any)
	if !ok {
		t.Fatal("obfuscate.source missing from CRD schema")
	}

	sourceRequired, ok := source["required"].([]any)
	if !ok || len(sourceRequired) != 1 || sourceRequired[0] != "claim" {
		t.Fatalf("obfuscate.source should require only claim when present, got %v", source["required"])
	}
}

func TestExampleMustGatherCRsWithoutObfuscateUnmarshal(t *testing.T) {
	examplesDir := filepath.Join(repoRoot(t), "examples")
	entries, err := os.ReadDir(examplesDir)
	if err != nil {
		t.Fatalf("read examples dir: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		if strings.HasPrefix(entry.Name(), "mustgather_obfuscate_") {
			continue
		}

		t.Run(entry.Name(), func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join(examplesDir, entry.Name()))
			if err != nil {
				t.Fatalf("read example: %v", err)
			}

			var mg MustGather
			if err := yaml.Unmarshal(content, &mg); err != nil {
				t.Fatalf("unmarshal example MustGather: %v", err)
			}

			if mg.Spec.Obfuscate != nil {
				t.Fatalf("example %s unexpectedly sets spec.obfuscate; examples must remain backward compatible", entry.Name())
			}

			if mg.Spec.ServiceAccountName == "" {
				t.Fatalf("example %s missing required serviceAccountName", entry.Name())
			}
		})
	}
}

func TestObfuscateCELRulesPresentInBundleCRD(t *testing.T) {
	crd := normalizeCEL(string(readCRDBytes(t, filepath.Join("bundle", "manifests", "tech-preview", "operator.openshift.io_mustgathers.yaml"))))

	for _, message := range []string{
		"obfuscate.enabled requires either obfuscate.source or uploadTarget",
		"obfuscate.source requires obfuscate.enabled to be true",
	} {
		if !strings.Contains(crd, message) {
			t.Fatalf("bundle CRD missing validation message %q", message)
		}
	}
}

// Existing storage.persistentVolume.subPath plus runtime directoryName isolation is unchanged;
// multi-run PVC path separation is verified in Phase 3 template tests (T3_4).
