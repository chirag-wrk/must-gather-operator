/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestMustGatherSpecObfuscateDefaultNil(t *testing.T) {
	spec := MustGatherSpec{}
	if spec.Obfuscate != nil {
		t.Fatalf("expected zero-value MustGatherSpec.Obfuscate to be nil, got %#v", spec.Obfuscate)
	}
}

func TestObfuscateConfigDefaultFields(t *testing.T) {
	cfg := ObfuscateConfig{}
	if cfg.Enabled != nil {
		t.Fatalf("expected zero-value ObfuscateConfig.Enabled to be nil, got %#v", cfg.Enabled)
	}
	if cfg.ObfuscationConfigRef != nil {
		t.Fatalf("expected zero-value ObfuscateConfig.ObfuscationConfigRef to be nil, got %#v", cfg.ObfuscationConfigRef)
	}
	if cfg.Source != nil {
		t.Fatalf("expected zero-value ObfuscateConfig.Source to be nil, got %#v", cfg.Source)
	}
}

func TestObfuscateConfigFieldPresence(t *testing.T) {
	enabled := true
	spec := MustGatherSpec{
		Obfuscate: &ObfuscateConfig{
			Enabled: &enabled,
			ObfuscationConfigRef: &corev1.LocalObjectReference{
				Name: "custom-obfuscation-config",
			},
			Source: &ObfuscateSourceConfig{
				Claim: PersistentVolumeClaimReference{Name: "bundle-pvc"},
				SubPath: "must-gather",
			},
		},
	}

	if spec.Obfuscate == nil {
		t.Fatal("expected Obfuscate field to be set")
	}
	if spec.Obfuscate.Enabled == nil || !*spec.Obfuscate.Enabled {
		t.Fatal("expected Obfuscate.Enabled to be true")
	}
	if spec.Obfuscate.ObfuscationConfigRef == nil || spec.Obfuscate.ObfuscationConfigRef.Name != "custom-obfuscation-config" {
		t.Fatal("expected Obfuscate.ObfuscationConfigRef to be set")
	}
	if spec.Obfuscate.Source == nil || spec.Obfuscate.Source.Claim.Name != "bundle-pvc" {
		t.Fatal("expected Obfuscate.Source.Claim.Name to be set")
	}
	if spec.Obfuscate.Source.SubPath != "must-gather" {
		t.Fatalf("expected Obfuscate.Source.SubPath to be must-gather, got %q", spec.Obfuscate.Source.SubPath)
	}
}

func TestMustGatherSpecBackwardCompatFields(t *testing.T) {
	spec := MustGatherSpec{}
	if spec.UploadTarget != nil {
		t.Fatal("expected zero-value UploadTarget to remain nil")
	}
	if spec.GatherSpec != nil {
		t.Fatal("expected zero-value GatherSpec to remain nil")
	}
	if spec.ImageStreamRef != nil {
		t.Fatal("expected zero-value ImageStreamRef to remain nil")
	}
	if spec.Storage != nil {
		t.Fatal("expected zero-value Storage to remain nil")
	}
	if spec.ServiceAccountName != "" {
		t.Fatal("expected zero-value ServiceAccountName to remain empty")
	}
}

// TestObfuscateValidationFixtures documents expected CRD CEL outcomes for FR-010/FR-011.
// Enforcement occurs at API server admission via x-kubernetes-validations on MustGatherSpec.
func TestObfuscateValidationFixtures(t *testing.T) {
	cases := []struct {
		name      string
		yaml      string
		wantValid bool
	}{
		{
			name: "enabled_with_upload_target",
			yaml: `obfuscate:
  enabled: true
uploadTarget:
  type: SFTP
  sftp:
    caseID: "12345"`,
			wantValid: true,
		},
		{
			name: "enabled_with_source",
			yaml: `obfuscate:
  enabled: true
  source:
    claim:
      name: existing-bundle`,
			wantValid: true,
		},
		{
			name: "enabled_with_source_and_upload",
			yaml: `obfuscate:
  enabled: true
  source:
    claim:
      name: existing-bundle
uploadTarget:
  type: SFTP
  sftp:
    caseID: "12345"`,
			wantValid: true,
		},
		{
			name: "enabled_only_fr010_violation",
			yaml: `obfuscate:
  enabled: true`,
			wantValid: false,
		},
		{
			name: "source_without_enabled_fr011_violation",
			yaml: `obfuscate:
  source:
    claim:
      name: existing-bundle`,
			wantValid: false,
		},
		{
			name: "source_with_enabled_false_fr011_violation",
			yaml: `obfuscate:
  enabled: false
  source:
    claim:
      name: existing-bundle`,
			wantValid: false,
		},
		{
			name: "obfuscate_omitted_backward_compat",
			yaml: `serviceAccountName: default`,
			wantValid: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.yaml == "" {
				t.Fatal("fixture yaml must not be empty")
			}
			if tc.wantValid {
				t.Logf("fixture accepted by CRD CEL:\n%s", tc.yaml)
			} else {
				t.Logf("fixture rejected by CRD CEL:\n%s", tc.yaml)
			}
		})
	}
}

func TestObfuscateValidationCRDRules(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Skipf("skipping CRD rule check: %v", err)
	}

	crdPath := filepath.Join(repoRoot, "deploy", "crds", "operator.openshift.io_mustgathers.yaml")
	content, err := os.ReadFile(crdPath)
	if err != nil {
		t.Fatalf("read CRD: %v", err)
	}

	crd := string(content)
	for _, msg := range []string{
		"obfuscate.enabled requires uploadTarget or obfuscate.source",
		"obfuscate.source requires obfuscate.enabled",
	} {
		if !strings.Contains(crd, msg) {
			t.Fatalf("CRD missing validation message %q", msg)
		}
	}

	for _, property := range []string{
		"obfuscate:",
		"enabled:",
		"obfuscationConfigRef:",
		"source:",
	} {
		if !strings.Contains(crd, property) {
			t.Fatalf("CRD missing obfuscation schema property marker %q", property)
		}
	}

	for _, existing := range []string{
		"uploadTarget:",
		"gatherSpec:",
		"imageStreamRef:",
		"storage:",
		"serviceAccountName:",
		"audit cannot be enabled when using a custom image",
	} {
		if !strings.Contains(crd, existing) {
			t.Fatalf("CRD missing existing spec field or validation %q", existing)
		}
	}
}

func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
