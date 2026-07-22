package mustgather

import (
	"context"
	"testing"

	mustgatherv1alpha1 "github.com/openshift/must-gather-operator/api/v1alpha1"
	"github.com/redhat-cop/operator-utils/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func TestObfuscationConditionConstantsMatchGodoc(t *testing.T) {
	if ConditionObfuscationConfigInvalid != "ObfuscationConfigInvalid" {
		t.Fatalf("unexpected ObfuscationConfigInvalid constant: %q", ConditionObfuscationConfigInvalid)
	}
	if ConditionObfuscationFailed != "ObfuscationFailed" {
		t.Fatalf("unexpected ObfuscationFailed constant: %q", ConditionObfuscationFailed)
	}
}

func TestSetObfuscationFailureStatus(t *testing.T) {
	tests := []struct {
		name          string
		conditionType string
		reason        string
		message       string
	}{
		{
			name:          "config invalid",
			conditionType: ConditionObfuscationConfigInvalid,
			reason:        "ConfigMapNotFound",
			message:       "obfuscation ConfigMap custom-rules not found in operator namespace",
		},
		{
			name:          "obfuscation failed",
			conditionType: ConditionObfuscationFailed,
			reason:        "JobFailed",
			message:       "MustGather Job failed during obfuscation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instance := &mustgatherv1alpha1.MustGather{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-mg",
					Namespace:  "test-ns",
					Generation: 2,
				},
			}

			s := runtime.NewScheme()
			if err := mustgatherv1alpha1.AddToScheme(s); err != nil {
				t.Fatalf("add scheme: %v", err)
			}

			cl := fake.NewClientBuilder().
				WithScheme(s).
				WithObjects(instance).
				WithStatusSubresource(&mustgatherv1alpha1.MustGather{}).
				Build()

			r := &MustGatherReconciler{
				ReconcilerBase: util.NewReconcilerBase(cl, s, &rest.Config{}, &record.FakeRecorder{}, nil),
			}

			_, err := r.setObfuscationFailureStatus(
				context.Background(),
				logf.Log.WithName("test"),
				instance,
				tt.conditionType,
				tt.reason,
				tt.message,
			)
			if err != nil {
				t.Fatalf("setObfuscationFailureStatus: %v", err)
			}

			if instance.Status.Status != "Failed" {
				t.Fatalf("expected Status Failed, got %q", instance.Status.Status)
			}
			if !instance.Status.Completed {
				t.Fatal("expected Completed true")
			}
			if instance.Status.Reason != tt.message {
				t.Fatalf("expected Reason %q, got %q", tt.message, instance.Status.Reason)
			}

			var found bool
			for _, cond := range instance.Status.Conditions {
				if cond.Type != tt.conditionType {
					continue
				}
				found = true
				if cond.Status != metav1.ConditionTrue {
					t.Fatalf("expected condition status True, got %v", cond.Status)
				}
				if cond.Reason != tt.reason {
					t.Fatalf("expected condition reason %q, got %q", tt.reason, cond.Reason)
				}
				if cond.Message != tt.message {
					t.Fatalf("expected condition message %q, got %q", tt.message, cond.Message)
				}
				if cond.ObservedGeneration != instance.GetGeneration() {
					t.Fatalf("expected ObservedGeneration %d, got %d", instance.GetGeneration(), cond.ObservedGeneration)
				}
			}
			if !found {
				t.Fatalf("expected condition type %q to be set", tt.conditionType)
			}

			for _, cond := range instance.Status.Conditions {
				if cond.Type == "ReconcileError" {
					t.Fatal("obfuscation failure must not set generic ReconcileError condition")
				}
			}
		})
	}
}
