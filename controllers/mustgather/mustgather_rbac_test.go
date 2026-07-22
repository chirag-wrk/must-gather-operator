package mustgather

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestOperatorClusterRole_ConfigMapReadForObfuscation(t *testing.T) {
	role := loadClusterRoleManifest(t, clusterRoleManifestPath())

	var configMapVerbs []string
	for _, rule := range role.Rules {
		if !slices.Contains(rule.Resources, "configmaps") {
			continue
		}
		if len(rule.APIGroups) == 0 || rule.APIGroups[0] != "" {
			continue
		}
		configMapVerbs = append(configMapVerbs, rule.Verbs...)
	}

	if len(configMapVerbs) == 0 {
		t.Fatal("expected ClusterRole rule for core configmaps")
	}

	for _, verb := range []string{"get", "list", "watch"} {
		if !ruleAllowsVerb(configMapVerbs, verb) {
			t.Fatalf("expected configmaps %q permission for obfuscationConfigRef validation, got verbs %v", verb, configMapVerbs)
		}
	}
}

func TestKubebuilderRBACMarker_IncludesConfigMapRead(t *testing.T) {
	source, err := os.ReadFile(controllerSourcePath())
	if err != nil {
		t.Fatalf("read controller source: %v", err)
	}

	content := string(source)
	if !strings.Contains(content, "resources=pods;services;services/finalizers;endpoints;persistentvolumeclaims;events;configmaps;secrets,verbs=get;list;watch") {
		t.Fatal("expected kubebuilder rbac marker granting configmaps get/list/watch")
	}
	if !strings.Contains(content, "obfuscationConfigRef") {
		t.Fatal("expected comment documenting obfuscation ConfigMap read RBAC")
	}
}

func clusterRoleManifestPath() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "deploy", "02_must-gather-operator.ClusterRole.yaml"))
}

func controllerSourcePath() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "mustgather_controller.go"))
}

func loadClusterRoleManifest(t *testing.T, path string) *rbacv1.ClusterRole {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ClusterRole manifest %q: %v", path, err)
	}

	role := &rbacv1.ClusterRole{}
	if err := yaml.Unmarshal(data, role); err != nil {
		t.Fatalf("unmarshal ClusterRole manifest: %v", err)
	}
	return role
}

func ruleAllowsVerb(ruleVerbs []string, verb string) bool {
	for _, v := range ruleVerbs {
		if v == "*" || v == verb {
			return true
		}
	}
	return false
}
