package mustgather

import (
	"context"
	"fmt"
	"strings"

	mustgatherv1alpha1 "github.com/openshift/must-gather-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// validateObfuscationConfigRef checks that a referenced custom obfuscation ConfigMap
// exists in the operator namespace and contains the required config.yaml key.
// Returns a non-empty reason and error for validation failures; returns ("", err)
// for transient API errors that should be retried.
func (r *MustGatherReconciler) validateObfuscationConfigRef(
	ctx context.Context,
	instance *mustgatherv1alpha1.MustGather,
) (reason string, err error) {
	if !obfuscateEnabled(instance.Spec.Obfuscate) {
		return "", nil
	}
	if instance.Spec.Obfuscate.ObfuscationConfigRef == nil {
		return "", nil
	}

	rawName := instance.Spec.Obfuscate.ObfuscationConfigRef.Name
	configMapName := strings.TrimSpace(rawName)
	if rawName != "" && configMapName == "" {
		return "InvalidConfigRef", fmt.Errorf("obfuscationConfigRef.name is whitespace-only")
	}
	if configMapName == "" {
		return "", nil
	}

	configMap := &corev1.ConfigMap{}
	err = r.GetClient().Get(ctx, types.NamespacedName{
		Namespace: r.OperatorNamespace,
		Name:      configMapName,
	}, configMap)
	if err != nil {
		if errors.IsNotFound(err) {
			return "ConfigMapNotFound", fmt.Errorf(
				"obfuscation ConfigMap %q not found in operator namespace %q",
				configMapName,
				r.OperatorNamespace,
			)
		}
		return "", err
	}

	if _, ok := configMap.Data[obfuscateConfigMapKey]; !ok {
		if _, ok := configMap.BinaryData[obfuscateConfigMapKey]; !ok {
			return "MissingConfigKey", fmt.Errorf(
				"obfuscation ConfigMap %q is missing required key %q",
				configMapName,
				obfuscateConfigMapKey,
			)
		}
	}

	return "", nil
}
