package v1alpha1

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

func TestObfuscateExampleCRsUnmarshalAndValidateShape(t *testing.T) {
	cases := []struct {
		name       string
		filename   string
		wantMode   string
		wantConfig bool
		wantSource bool
		wantSC     []string
	}{
		{
			name:     "default policy and upload",
			filename: "mustgather_obfuscate_default_upload.yaml",
			wantMode: "1",
			wantSC:   []string{"SC-001", "SC-002", "SC-003"},
		},
		{
			name:       "custom obfuscationConfigRef and upload",
			filename:   "mustgather_obfuscate_custom_config.yaml",
			wantMode:   "1",
			wantConfig: true,
			wantSC:     []string{"SC-004"},
		},
		{
			name:       "source PVC and upload",
			filename:   "mustgather_obfuscate_source_pvc.yaml",
			wantMode:   "2",
			wantSource: true,
			wantSC:     []string{"SC-005"},
		},
	}

	examplesDir := filepath.Join(repoRoot(t), "examples")

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(examplesDir, tc.filename)
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read example: %v", err)
			}

			commentBlock := stringBeforeYAMLDocument(content)
			if !strings.Contains(commentBlock, "Mode "+tc.wantMode) {
				t.Fatalf("example comments should document Mode %s", tc.wantMode)
			}
			for _, sc := range tc.wantSC {
				if !strings.Contains(commentBlock, sc) {
					t.Fatalf("example comments missing spec scenario %s", sc)
				}
			}

			var mg MustGather
			if err := yaml.Unmarshal(content, &mg); err != nil {
				t.Fatalf("unmarshal example MustGather: %v", err)
			}

			if mg.Spec.ServiceAccountName == "" {
				t.Fatal("example missing serviceAccountName")
			}

			if mg.Spec.Obfuscate == nil {
				t.Fatal("example missing spec.obfuscate")
			}

			if mg.Spec.Obfuscate.Enabled == nil || !*mg.Spec.Obfuscate.Enabled {
				t.Fatal("example must set obfuscate.enabled: true")
			}

			if mg.Spec.UploadTarget == nil {
				t.Fatal("example missing uploadTarget; obfuscate-only without upload is invalid")
			}

			if tc.wantConfig {
				if mg.Spec.Obfuscate.ObfuscationConfigRef == nil || mg.Spec.Obfuscate.ObfuscationConfigRef.Name == "" {
					t.Fatal("example missing obfuscationConfigRef.name")
				}
			}

			if tc.wantSource {
				if mg.Spec.Obfuscate.Source == nil {
					t.Fatal("example missing obfuscate.source")
				}
				if mg.Spec.Obfuscate.Source.Claim.Name == "" {
					t.Fatal("example missing obfuscate.source.claim.name")
				}
				if mg.Spec.Obfuscate.Source.SubPath == "" {
					t.Fatal("example must show a valid non-empty subPath when subPath is set")
				}
			}

			if err := validateObfuscateCEL(&mg.Spec); err != nil {
				t.Fatalf("example violates obfuscate CEL rules: %v", err)
			}
		})
	}
}

func TestObfuscateExampleFieldNamesMatchAPIJSONTags(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "examples", "mustgather_obfuscate_custom_config.yaml"))
	if err != nil {
		t.Fatalf("read example: %v", err)
	}

	content := string(raw)
	for _, tag := range []string{
		"obfuscate:",
		"enabled:",
		"obfuscationConfigRef:",
		"uploadTarget:",
		"serviceAccountName:",
	} {
		if !strings.Contains(content, tag) {
			t.Fatalf("example missing json-tagged field key %q", tag)
		}
	}
}

func TestObfuscateExamplesREADMEDocumentsThreeModes(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join(repoRoot(t), "examples", "README.md"))
	if err != nil {
		t.Fatalf("read examples README: %v", err)
	}

	content := string(readme)
	for _, fragment := range []string{
		"mustgather_obfuscate_default_upload.yaml",
		"mustgather_obfuscate_custom_config.yaml",
		"mustgather_obfuscate_source_pvc.yaml",
		"SC-001",
		"SC-004",
		"SC-005",
		"must-gather-operator",
		"config.yaml",
	} {
		if !strings.Contains(content, fragment) {
			t.Fatalf("examples/README.md missing %q", fragment)
		}
	}
}

// validateObfuscateCEL mirrors FR-012 and FR-013 CEL rules embedded in the MustGather CRD.
func validateObfuscateCEL(spec *MustGatherSpec) error {
	if spec == nil || spec.Obfuscate == nil {
		return nil
	}

	ob := spec.Obfuscate
	enabled := ob.Enabled != nil && *ob.Enabled
	hasSource := ob.Source != nil
	hasUpload := spec.UploadTarget != nil

	if enabled && !hasSource && !hasUpload {
		return errObfuscateCEL("obfuscate.enabled requires either obfuscate.source or uploadTarget")
	}

	if hasSource && !enabled {
		return errObfuscateCEL("obfuscate.source requires obfuscate.enabled to be true")
	}

	if hasSource && ob.Source.SubPath != "" && strings.TrimSpace(ob.Source.SubPath) == "" {
		return errObfuscateCEL("obfuscate.source.subPath must not be whitespace-only when set")
	}

	return nil
}

type obfuscateCELError string

func (e obfuscateCELError) Error() string { return string(e) }

func errObfuscateCEL(msg string) error { return obfuscateCELError(msg) }

func stringBeforeYAMLDocument(content []byte) string {
	doc := strings.Index(string(content), "apiVersion:")
	if doc == -1 {
		return string(content)
	}
	return string(content[:doc])
}
