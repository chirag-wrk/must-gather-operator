package mustgather

import (
	"context"
	"os"
	"strings"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	mustgatherv1alpha1 "github.com/openshift/must-gather-operator/api/v1alpha1"
	mgconfig "github.com/openshift/must-gather-operator/config"
	"github.com/redhat-cop/operator-utils/pkg/util"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Obfuscation upload env injection (obfuscate=true, obfuscate_config) is covered by
// Test_getUploadContainer_ObfuscateEnv in template_test.go — env vars are injected only
// when obfuscate.enabled is true (Phase 3 contract).

func TestGetJobFromInstance_OperatorImageEnvValidation(t *testing.T) {
	instance := &mustgatherv1alpha1.MustGather{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mg-env-test",
			Namespace: testCRNamespace,
		},
		Spec: mustgatherv1alpha1.MustGatherSpec{},
	}
	r := newImageTestReconciler(t, []client.Object{instance}, interceptClient{})

	tests := []struct {
		name        string
		setupEnv    func(t *testing.T)
		wantErrPart string
	}{
		{
			name: "missing operator image",
			setupEnv: func(t *testing.T) {
				t.Helper()
				if err := os.Unsetenv("OPERATOR_IMAGE"); err != nil {
					t.Fatalf("unset OPERATOR_IMAGE: %v", err)
				}
			},
			wantErrPart: "operator image environment variable not found or empty",
		},
		{
			name: "empty operator image",
			setupEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("OPERATOR_IMAGE", "")
			},
			wantErrPart: "operator image environment variable not found or empty",
		},
		{
			name: "whitespace operator image",
			setupEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("OPERATOR_IMAGE", "   ")
			},
			wantErrPart: "operator image environment variable not found or empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupEnv(t)
			_, err := r.getJobFromInstance(context.Background(), logf.Log, instance)
			if err == nil {
				t.Fatal("expected error but got none")
			}
			if !strings.Contains(err.Error(), tt.wantErrPart) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErrPart, err)
			}
		})
	}
}

func TestReconcile_OperatorImageEnvValidation(t *testing.T) {
	const mgName = "mg-env-reconcile"

	baseObjects := func() []client.Object {
		return []client.Object{
			&mustgatherv1alpha1.MustGather{
				ObjectMeta: metav1.ObjectMeta{
					Name:       mgName,
					Namespace:  testCRNamespace,
					Finalizers: []string{mustGatherFinalizer},
				},
				Spec: mustgatherv1alpha1.MustGatherSpec{
					ServiceAccountName: "default",
				},
			},
			&corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: testCRNamespace},
			},
			&configv1.ClusterVersion{
				ObjectMeta: metav1.ObjectMeta{Name: "version"},
				Status: configv1.ClusterVersionStatus{
					History: []configv1.UpdateHistory{{State: "Completed", Version: "4.14.0"}},
				},
			},
		}
	}

	tests := []struct {
		name        string
		setupEnv    func(t *testing.T)
		wantErrPart string
	}{
		{
			name: "missing operator image blocks job creation",
			setupEnv: func(t *testing.T) {
				t.Helper()
				if err := os.Unsetenv("OPERATOR_IMAGE"); err != nil {
					t.Fatalf("unset OPERATOR_IMAGE: %v", err)
				}
			},
			wantErrPart: "operator image environment variable not found or empty",
		},
		{
			name: "empty operator image blocks job creation",
			setupEnv: func(t *testing.T) {
				t.Helper()
				t.Setenv("OPERATOR_IMAGE", "")
			},
			wantErrPart: "operator image environment variable not found or empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := runtime.NewScheme()
			_ = corev1.AddToScheme(s)
			_ = batchv1.AddToScheme(s)
			_ = mustgatherv1alpha1.AddToScheme(s)
			_ = configv1.AddToScheme(s)

			cl := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(baseObjects()...).
				WithStatusSubresource(&mustgatherv1alpha1.MustGather{}).
				Build()

			tt.setupEnv(t)
			t.Setenv("DEFAULT_MUST_GATHER_IMAGE", testDefaultMustGatherImage)
			t.Setenv("OPERATOR_NAMESPACE", testOperatorNamespace)

			r := &MustGatherReconciler{
				ReconcilerBase:             util.NewReconcilerBase(cl, s, &rest.Config{}, &record.FakeRecorder{}, nil),
				DefaultMustGatherImage:     testDefaultMustGatherImage,
				OperatorNamespace:          testOperatorNamespace,
				OperatorServiceAccountName: mgconfig.OperatorName,
			}

			_, err := r.Reconcile(context.Background(), reconcile.Request{
				NamespacedName: types.NamespacedName{Name: mgName, Namespace: testCRNamespace},
			})
			if err == nil {
				t.Fatal("expected reconcile error but got none")
			}
			if !strings.Contains(err.Error(), tt.wantErrPart) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErrPart, err)
			}

			job := &batchv1.Job{}
			if jobErr := cl.Get(context.Background(), types.NamespacedName{Name: mgName, Namespace: testCRNamespace}, job); jobErr == nil {
				t.Fatal("expected job not to be created when OPERATOR_IMAGE is invalid")
			}
		})
	}
}
