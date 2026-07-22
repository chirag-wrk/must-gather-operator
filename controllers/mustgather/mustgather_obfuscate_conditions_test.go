package mustgather

import (
	"context"
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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestReconcileObfuscationConfigValidation(t *testing.T) {
	const (
		operatorNs   = "must-gather-operator"
		customerNs   = "customer-ns"
		configMapRef = "custom-obfuscation-rules"
		mgName       = "obfuscate-mg"
	)

	enabled := true
	baseMG := func() *mustgatherv1alpha1.MustGather {
		return &mustgatherv1alpha1.MustGather{
			ObjectMeta: metav1.ObjectMeta{
				Name:       mgName,
				Namespace:  customerNs,
				Finalizers: []string{mustGatherFinalizer},
				Generation: 1,
			},
			Spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
				Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
					Enabled: &enabled,
					ObfuscationConfigRef: &corev1.LocalObjectReference{
						Name: configMapRef,
					},
				},
				UploadTarget: &mustgatherv1alpha1.UploadTargetSpec{
					Type: mustgatherv1alpha1.UploadTypeSFTP,
					SFTP: &mustgatherv1alpha1.SFTPSpec{
						CaseID: "12345678",
						CaseManagementAccountSecretRef: corev1.LocalObjectReference{
							Name: "case-secret",
						},
					},
				},
			},
		}
	}

	clusterVersion := func() *configv1.ClusterVersion {
		return &configv1.ClusterVersion{
			ObjectMeta: metav1.ObjectMeta{Name: "version"},
			Status: configv1.ClusterVersionStatus{
				History: []configv1.UpdateHistory{{State: "Completed", Version: "4.14.0"}},
			},
		}
	}

	tests := []struct {
		name                string
		extraObjects        []client.Object
		wantConditionType   string
		wantConditionReason string
		wantMessageContains string
		expectJobCreated    bool
	}{
		{
			name:                "missing_config_map_sets_obfuscation_config_invalid",
			extraObjects:        nil,
			wantConditionType:   ConditionObfuscationConfigInvalid,
			wantConditionReason: "ConfigMapNotFound",
			wantMessageContains: configMapRef,
			expectJobCreated:    false,
		},
		{
			name: "config_map_missing_config_yaml_key_sets_obfuscation_config_invalid",
			extraObjects: []client.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: configMapRef, Namespace: operatorNs},
					Data:       map[string]string{"rules.yaml": "config:\n  obfuscate: []"},
				},
			},
			wantConditionType:   ConditionObfuscationConfigInvalid,
			wantConditionReason: "MissingConfigKey",
			wantMessageContains: obfuscateConfigMapKey,
			expectJobCreated:    false,
		},
		{
			name: "valid_config_map_allows_job_creation",
			extraObjects: []client.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: configMapRef, Namespace: operatorNs},
					Data: map[string]string{
						obfuscateConfigMapKey: "config:\n  obfuscate: []",
					},
				},
			},
			expectJobCreated: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := runtime.NewScheme()
			_ = corev1.AddToScheme(s)
			_ = batchv1.AddToScheme(s)
			_ = mustgatherv1alpha1.AddToScheme(s)
			_ = configv1.AddToScheme(s)

			objects := []client.Object{
				baseMG(),
				&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: customerNs}},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "case-secret", Namespace: customerNs},
					Data: map[string][]byte{
						"username": []byte("user"),
						"password": []byte("pass"),
					},
				},
				clusterVersion(),
			}
			objects = append(objects, tt.extraObjects...)

			cl := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(objects...).
				WithStatusSubresource(&mustgatherv1alpha1.MustGather{}).
				Build()

			originalSftpDialFunc := sftpDialFunc
			defer func() { sftpDialFunc = originalSftpDialFunc }()
			sftpDialFunc = func(ctx context.Context, username, password, host string) error {
				return nil
			}

			t.Setenv("OPERATOR_IMAGE", "test-operator-image")
			t.Setenv("DEFAULT_MUST_GATHER_IMAGE", "test-must-gather-image")
			t.Setenv("OPERATOR_NAMESPACE", operatorNs)

			r := &MustGatherReconciler{
				ReconcilerBase:             util.NewReconcilerBase(cl, s, &rest.Config{}, &record.FakeRecorder{}, nil),
				DefaultMustGatherImage:     "test-must-gather-image",
				OperatorNamespace:          operatorNs,
				OperatorServiceAccountName: mgconfig.OperatorName,
			}

			res, err := r.Reconcile(context.Background(), reconcile.Request{
				NamespacedName: types.NamespacedName{Name: mgName, Namespace: customerNs},
			})
			if err != nil {
				t.Fatalf("unexpected reconcile error: %v", err)
			}
			if res.Requeue || res.RequeueAfter > 0 {
				t.Fatalf("expected empty reconcile result, got %+v", res)
			}

			out := &mustgatherv1alpha1.MustGather{}
			if getErr := cl.Get(context.Background(), types.NamespacedName{Name: mgName, Namespace: customerNs}, out); getErr != nil {
				t.Fatalf("failed to get mustgather: %v", getErr)
			}

			job := &batchv1.Job{}
			jobErr := cl.Get(context.Background(), types.NamespacedName{Name: mgName, Namespace: customerNs}, job)
			if tt.expectJobCreated {
				if jobErr != nil {
					t.Fatalf("expected job to be created: %v", jobErr)
				}
				return
			}
			if jobErr == nil {
				t.Fatal("expected job not to be created")
			}

			if out.Status.Status != "Failed" || !out.Status.Completed {
				t.Fatalf("expected failed completed status, got %+v", out.Status)
			}

			assertObfuscationCondition(
				t,
				out.Status.Conditions,
				tt.wantConditionType,
				tt.wantConditionReason,
				tt.wantMessageContains,
			)
		})
	}
}

func assertObfuscationCondition(
	t *testing.T,
	conditions []metav1.Condition,
	wantType string,
	wantReason string,
	wantMessageContains string,
) {
	t.Helper()

	var found bool
	for _, cond := range conditions {
		if cond.Type == "ReconcileError" {
			t.Fatal("obfuscation failure must not set generic ReconcileError condition")
		}
		if cond.Type != wantType {
			continue
		}
		found = true
		if cond.Status != metav1.ConditionTrue {
			t.Fatalf("expected %s status True, got %v", wantType, cond.Status)
		}
		if cond.Reason != wantReason {
			t.Fatalf("expected reason %q, got %q", wantReason, cond.Reason)
		}
		if wantMessageContains != "" && !strings.Contains(cond.Message, wantMessageContains) {
			t.Fatalf("expected message to contain %q, got %q", wantMessageContains, cond.Message)
		}
	}
	if !found {
		t.Fatalf("expected %s condition to be set", wantType)
	}
}
