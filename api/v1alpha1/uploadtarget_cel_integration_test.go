package v1alpha1

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

const (
	expectedUploadTargetCELRule    = `has(self.type) && self.type == 'SFTP' ? has(self.sftp) : !has(self.sftp)`
	expectedUploadTargetCELMessage = "sftp upload target config is required when upload type is SFTP"
)

// uploadTargetCELAllows mirrors the CRD CEL rule for uploadTarget admission validation.
func uploadTargetCELAllows(target map[string]any) bool {
	typeVal, hasType := target["type"].(string)
	_, hasSFTP := target["sftp"]
	if hasType && typeVal == string(UploadTypeSFTP) {
		return hasSFTP
	}
	return !hasSFTP
}

func loadMustGatherCRD(t *testing.T, relPath string) *apiextensionsv1.CustomResourceDefinition {
	t.Helper()

	crdPath := filepath.Join("..", "..", relPath)
	data, err := os.ReadFile(crdPath)
	if err != nil {
		t.Fatalf("read CRD %s: %v", crdPath, err)
	}

	crd := &apiextensionsv1.CustomResourceDefinition{}
	if err := yaml.Unmarshal(data, crd); err != nil {
		t.Fatalf("unmarshal CRD %s: %v", crdPath, err)
	}
	return crd
}

func uploadTargetValidations(t *testing.T, crd *apiextensionsv1.CustomResourceDefinition) []apiextensionsv1.ValidationRule {
	t.Helper()

	if len(crd.Spec.Versions) == 0 {
		t.Fatal("CRD has no versions")
	}

	schema := crd.Spec.Versions[0].Schema
	if schema == nil || schema.OpenAPIV3Schema == nil {
		t.Fatal("CRD missing openAPIV3Schema")
	}

	specSchema, ok := schema.OpenAPIV3Schema.Properties["spec"]
	if !ok {
		t.Fatal("CRD missing spec schema")
	}

	uploadTargetSchema, ok := specSchema.Properties["uploadTarget"]
	if !ok {
		t.Fatal("CRD missing spec.uploadTarget schema")
	}

	if len(uploadTargetSchema.XValidations) == 0 {
		t.Fatal("CRD missing uploadTarget x-kubernetes-validations")
	}

	return uploadTargetSchema.XValidations
}

func TestMustGatherCRD_UploadTargetCELRule(t *testing.T) {
	crd := loadMustGatherCRD(t, filepath.Join("deploy", "crds", "operator.openshift.io_mustgathers.yaml"))
	validations := uploadTargetValidations(t, crd)

	found := false
	for _, v := range validations {
		if strings.Contains(v.Message, expectedUploadTargetCELMessage) {
			found = true
			if v.Rule != expectedUploadTargetCELRule {
				t.Fatalf("unexpected CEL rule:\ngot:  %q\nwant: %q", v.Rule, expectedUploadTargetCELRule)
			}
		}
	}
	if !found {
		t.Fatalf("CRD missing expected uploadTarget CEL validation message %q", expectedUploadTargetCELMessage)
	}

	specSchema := crd.Spec.Versions[0].Schema.OpenAPIV3Schema.Properties["spec"]
	for _, req := range specSchema.Required {
		if req == "caseID" || req == "caseManagementAccountSecretRef" {
			t.Fatalf("legacy upload field %q still required at spec level", req)
		}
	}
}

func TestMustGatherCRD_BundleMatchesDeploy(t *testing.T) {
	deploy := loadMustGatherCRD(t, filepath.Join("deploy", "crds", "operator.openshift.io_mustgathers.yaml"))
	bundle := loadMustGatherCRD(t, filepath.Join("bundle", "manifests", "tech-preview", "operator.openshift.io_mustgathers.yaml"))

	deployRules := uploadTargetValidations(t, deploy)
	bundleRules := uploadTargetValidations(t, bundle)

	if len(deployRules) != len(bundleRules) {
		t.Fatalf("validation rule count mismatch: deploy=%d bundle=%d", len(deployRules), len(bundleRules))
	}
	for i := range deployRules {
		if deployRules[i].Rule != bundleRules[i].Rule || deployRules[i].Message != bundleRules[i].Message {
			t.Fatalf("validation rule mismatch at index %d", i)
		}
	}
}

func TestUploadTargetCEL_RejectsInvalidCombinations(t *testing.T) {
	validSFTP := map[string]any{
		"type": string(UploadTypeSFTP),
		"sftp": map[string]any{
			"caseID": "01234567",
			"caseManagementAccountSecretRef": map[string]any{
				"name": "case-management-creds",
			},
		},
	}

	tests := []struct {
		name    string
		target  map[string]any
		allowed bool
	}{
		{
			name:    "SFTP type with sftp block is valid",
			target:  validSFTP,
			allowed: true,
		},
		{
			name: "SFTP type without sftp block is rejected",
			target: map[string]any{
				"type": string(UploadTypeSFTP),
			},
			allowed: false,
		},
		{
			name: "sftp block without SFTP type is rejected",
			target: map[string]any{
				"sftp": validSFTP["sftp"],
			},
			allowed: false,
		},
	}

	crd := loadMustGatherCRD(t, filepath.Join("deploy", "crds", "operator.openshift.io_mustgathers.yaml"))
	_ = uploadTargetValidations(t, crd)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := uploadTargetCELAllows(tt.target)
			if got != tt.allowed {
				t.Fatalf("admission would allow=%v, want allow=%v for %#v", got, tt.allowed, tt.target)
			}
		})
	}
}
