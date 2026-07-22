//go:build e2e
// +build e2e

package e2e

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestObfuscateTestdataEmbedded(t *testing.T) {
	cases := []struct {
		fixture     ObfuscateConfigMapFixture
		wantName    string
		wantConfig  bool
		description string
	}{
		{
			fixture:     ObfuscateConfigMapValid,
			wantName:    ObfuscationConfigMapValidName,
			wantConfig:  true,
			description: "valid policy with config.yaml key",
		},
		{
			fixture:     ObfuscateConfigMapInvalid,
			wantName:    ObfuscationConfigMapInvalidName,
			wantConfig:  false,
			description: "invalid policy missing config.yaml key",
		},
		{
			fixture:     ObfuscateConfigMapMACDisabled,
			wantName:    ObfuscationConfigMapMACDisabledName,
			wantConfig:  true,
			description: "SC-004 custom policy without MAC obfuscation",
		},
	}

	for _, tc := range cases {
		t.Run(tc.description, func(t *testing.T) {
			content, err := testassets.ReadFile(filepath.Join("testdata", tc.fixture.filename()))
			if err != nil {
				t.Fatalf("read embedded testdata: %v", err)
			}

			raw := string(content)
			if !strings.Contains(raw, "name: "+tc.wantName) {
				t.Fatalf("testdata missing ConfigMap name %q", tc.wantName)
			}
			if !strings.Contains(raw, "namespace: "+operatorNamespace) {
				t.Fatalf("testdata missing operator namespace %q", operatorNamespace)
			}

			hasConfigKey := strings.Contains(raw, obfuscateConfigMapKey+":")
			if hasConfigKey != tc.wantConfig {
				t.Fatalf("config.yaml key present = %v, want %v", hasConfigKey, tc.wantConfig)
			}
		})
	}
}

func TestBuildObfuscateMustGatherSpec(t *testing.T) {
	enabled := true
	mg := buildMustGatherCR("mg-obf", "test-ns", serviceAccount, false, &MustGatherCROptions{
		Obfuscate: &ObfuscateOptions{
			Enabled:          &enabled,
			ConfigMapRefName: ObfuscationConfigMapValidName,
			SourcePVCName:    mustGatherPVCName,
			SourceSubPath:    "bundle-subpath",
		},
		UploadTarget: &UploadTargetOptions{
			CaseID:     "12345",
			SecretName: caseManagementSecretNameValid,
		},
	})

	if mg.Spec.Obfuscate == nil || mg.Spec.Obfuscate.Enabled == nil || !*mg.Spec.Obfuscate.Enabled {
		t.Fatal("expected obfuscate.enabled true")
	}
	if mg.Spec.Obfuscate.ObfuscationConfigRef == nil || mg.Spec.Obfuscate.ObfuscationConfigRef.Name != ObfuscationConfigMapValidName {
		t.Fatalf("unexpected obfuscationConfigRef: %#v", mg.Spec.Obfuscate.ObfuscationConfigRef)
	}
	if mg.Spec.Obfuscate.Source == nil || mg.Spec.Obfuscate.Source.Claim.Name != mustGatherPVCName {
		t.Fatalf("unexpected source claim: %#v", mg.Spec.Obfuscate.Source)
	}
	if mg.Spec.UploadTarget == nil {
		t.Fatal("expected uploadTarget on obfuscate MustGather helper shape")
	}
}

func TestBuildObfuscateSourceModeSpec(t *testing.T) {
	enabled := true
	mg := buildMustGatherCR("mg-source", "test-ns", serviceAccount, false, &MustGatherCROptions{
		Obfuscate: &ObfuscateOptions{
			Enabled:       &enabled,
			SourcePVCName: mustGatherPVCName,
			SourceSubPath: obfuscateSourceBundleSubPath,
		},
	})

	if mg.Spec.Obfuscate == nil || mg.Spec.Obfuscate.Source == nil {
		t.Fatal("expected obfuscate.source")
	}
	if mg.Spec.Obfuscate.Source.Claim.Name != mustGatherPVCName {
		t.Fatalf("unexpected source claim: %#v", mg.Spec.Obfuscate.Source)
	}
	if mg.Spec.Obfuscate.Source.SubPath != obfuscateSourceBundleSubPath {
		t.Fatalf("unexpected source subPath: %q", mg.Spec.Obfuscate.Source.SubPath)
	}
	if mg.Spec.UploadTarget != nil {
		t.Fatal("Mode 2 helper shape should omit uploadTarget when not set")
	}
}

func TestBuildObfuscateCustomConfigMustGatherSpec(t *testing.T) {
	enabled := true
	mg := buildMustGatherCR("mg-invalid-cm", "test-ns", serviceAccount, false, &MustGatherCROptions{
		Obfuscate: &ObfuscateOptions{
			Enabled:          &enabled,
			ConfigMapRefName: ObfuscationConfigMapInvalidName,
			SourcePVCName:    mustGatherPVCName,
			SourceSubPath:    obfuscateSourceBundleSubPath,
		},
	})

	if mg.Spec.Obfuscate == nil || mg.Spec.Obfuscate.ObfuscationConfigRef == nil {
		t.Fatal("expected obfuscationConfigRef")
	}
	if mg.Spec.Obfuscate.ObfuscationConfigRef.Name != ObfuscationConfigMapInvalidName {
		t.Fatalf("unexpected config ref: %q", mg.Spec.Obfuscate.ObfuscationConfigRef.Name)
	}
	if mg.Spec.UploadTarget != nil {
		t.Fatal("custom config negative tests should omit uploadTarget to avoid SFTP pre-check")
	}
}
