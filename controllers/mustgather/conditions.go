package mustgather

import (
	"context"

	"github.com/go-logr/logr"
	mustgatherv1alpha1 "github.com/openshift/must-gather-operator/api/v1alpha1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// ConditionObfuscationConfigInvalid indicates the referenced obfuscation ConfigMap
	// is missing or lacks a valid config.yaml key.
	ConditionObfuscationConfigInvalid = "ObfuscationConfigInvalid"

	// ConditionObfuscationFailed indicates obfuscation failed during Job execution.
	ConditionObfuscationFailed = "ObfuscationFailed"
)

// setObfuscationFailureStatus records a distinct obfuscation failure condition on the
// MustGather CR. conditionType must be ConditionObfuscationConfigInvalid or
// ConditionObfuscationFailed — not the generic ReconcileError type.
func (r *MustGatherReconciler) setObfuscationFailureStatus(
	ctx context.Context,
	reqLogger logr.Logger,
	instance *mustgatherv1alpha1.MustGather,
	conditionType string,
	reason string,
	message string,
) (reconcile.Result, error) {
	instance.Status.Status = "Failed"
	instance.Status.Completed = true
	instance.Status.Reason = message
	instance.Status.LastUpdate = metav1.Now()

	apimeta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             metav1.ConditionTrue,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: instance.GetGeneration(),
	})

	r.GetRecorder().Event(instance, "Warning", "ProcessingError", message)

	if statusErr := r.GetClient().Status().Update(ctx, instance); statusErr != nil {
		reqLogger.Error(statusErr, "failed to update status after obfuscation failure")
		return r.ManageError(ctx, instance, statusErr)
	}
	return reconcile.Result{}, nil
}
