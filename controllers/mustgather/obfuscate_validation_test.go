package mustgather

import (
	"context"
	"testing"

	mustgatherv1alpha1 "github.com/openshift/must-gather-operator/api/v1alpha1"
	"github.com/redhat-cop/operator-utils/pkg/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestValidateObfuscationConfigRef(t *testing.T) {
	const operatorNs = "must-gather-operator"
	enabled := true

	tests := []struct {
		name       string
		spec       mustgatherv1alpha1.MustGatherSpec
		objects    []client.Object
		wantReason string
		wantErr    bool
	}{
		{
			name: "obfuscate disabled skips validation",
			spec: mustgatherv1alpha1.MustGatherSpec{},
		},
		{
			name: "no custom config ref uses default",
			spec: mustgatherv1alpha1.MustGatherSpec{
				Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{Enabled: &enabled},
			},
		},
		{
			name: "empty config ref name skips validation",
			spec: mustgatherv1alpha1.MustGatherSpec{
				Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
					Enabled:              &enabled,
					ObfuscationConfigRef: &corev1.LocalObjectReference{Name: ""},
				},
			},
		},
		{
			name: "whitespace config ref name is invalid",
			spec: mustgatherv1alpha1.MustGatherSpec{
				Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
					Enabled:              &enabled,
					ObfuscationConfigRef: &corev1.LocalObjectReference{Name: "   "},
				},
			},
			wantReason: "InvalidConfigRef",
			wantErr:    true,
		},
		{
			name: "missing config map",
			spec: mustgatherv1alpha1.MustGatherSpec{
				Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
					Enabled:              &enabled,
					ObfuscationConfigRef: &corev1.LocalObjectReference{Name: "custom-rules"},
				},
			},
			wantReason: "ConfigMapNotFound",
			wantErr:    true,
		},
		{
			name: "config map missing config.yaml key",
			spec: mustgatherv1alpha1.MustGatherSpec{
				Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
					Enabled:              &enabled,
					ObfuscationConfigRef: &corev1.LocalObjectReference{Name: "custom-rules"},
				},
			},
			objects: []client.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: "custom-rules", Namespace: operatorNs},
					Data:       map[string]string{"other.yaml": "x"},
				},
			},
			wantReason: "MissingConfigKey",
			wantErr:    true,
		},
		{
			name: "valid config map",
			spec: mustgatherv1alpha1.MustGatherSpec{
				Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
					Enabled:              &enabled,
					ObfuscationConfigRef: &corev1.LocalObjectReference{Name: "custom-rules"},
				},
			},
			objects: []client.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: "custom-rules", Namespace: operatorNs},
					Data:       map[string]string{obfuscateConfigMapKey: "config:\n  obfuscate: []"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := runtime.NewScheme()
			_ = corev1.AddToScheme(s)
			_ = mustgatherv1alpha1.AddToScheme(s)

			instance := &mustgatherv1alpha1.MustGather{
				ObjectMeta: metav1.ObjectMeta{Name: "mg", Namespace: "customer-ns"},
				Spec:       tt.spec,
			}

			objs := append([]client.Object{instance}, tt.objects...)
			cl := fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
			r := &MustGatherReconciler{
				ReconcilerBase:    util.NewReconcilerBase(cl, s, &rest.Config{}, &record.FakeRecorder{}, nil),
				OperatorNamespace: operatorNs,
			}

			reason, err := r.validateObfuscationConfigRef(context.Background(), instance)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if reason != tt.wantReason {
					t.Fatalf("reason = %q, want %q", reason, tt.wantReason)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if reason != "" {
				t.Fatalf("expected empty reason, got %q", reason)
			}
		})
	}
}
