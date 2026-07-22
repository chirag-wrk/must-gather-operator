//go:build e2e
// +build e2e

package e2e

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	mustgatherv1alpha1 "github.com/openshift/must-gather-operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	obfuscateConfigMapKey = "config.yaml"

	obfuscationConfigMapValidFilename       = "obfuscation-configmap-valid.yaml"
	obfuscationConfigMapInvalidFilename     = "obfuscation-configmap-invalid.yaml"
	obfuscationConfigMapMACDisabledFilename = "obfuscation-configmap-mac-disabled.yaml"

	ObfuscationConfigMapValidName       = "must-gather-clean-config-e2e"
	ObfuscationConfigMapInvalidName     = "must-gather-clean-config-invalid-e2e"
	ObfuscationConfigMapMACDisabledName = "must-gather-clean-config-mac-disabled-e2e"

	uploadEnvObfuscateName       = "obfuscate"
	uploadEnvObfuscateConfigName = "obfuscate_config"
	defaultObfuscateConfigPath   = "/etc/must-gather-clean/default-config.yaml"

	obfuscateLogRunningMarker   = "Running obfuscation"
	obfuscateLogCompleteMarker  = "Obfuscation complete"
	obfuscateLogNoUploadMarker    = "Obfuscation complete; no upload target configured"

	obfuscateSourceVolumeNameE2E = "obfuscate-source"
	obfuscateSourceBundleSubPath = "source-bundle"
	sourcePVCMarkerFileName      = ".obfuscate-e2e-marker"

	conditionObfuscationConfigInvalidE2E = "ObfuscationConfigInvalid"
	obfuscationReasonConfigMapNotFound   = "ConfigMapNotFound"
	obfuscationReasonMissingConfigKey    = "MissingConfigKey"
)

// ObfuscateOptions configures obfuscation on a MustGather CR for E2E scenarios.
type ObfuscateOptions struct {
	Enabled          *bool
	ConfigMapRefName string
	SourcePVCName    string
	SourceSubPath    string
}

// ObfuscateConfigMapFixture selects embedded operator-namespace ConfigMap testdata.
type ObfuscateConfigMapFixture int

const (
	ObfuscateConfigMapValid ObfuscateConfigMapFixture = iota
	ObfuscateConfigMapInvalid
	ObfuscateConfigMapMACDisabled
)

func (f ObfuscateConfigMapFixture) filename() string {
	switch f {
	case ObfuscateConfigMapValid:
		return obfuscationConfigMapValidFilename
	case ObfuscateConfigMapInvalid:
		return obfuscationConfigMapInvalidFilename
	case ObfuscateConfigMapMACDisabled:
		return obfuscationConfigMapMACDisabledFilename
	default:
		return obfuscationConfigMapValidFilename
	}
}

func (f ObfuscateConfigMapFixture) name() string {
	switch f {
	case ObfuscateConfigMapValid:
		return ObfuscationConfigMapValidName
	case ObfuscateConfigMapInvalid:
		return ObfuscationConfigMapInvalidName
	case ObfuscateConfigMapMACDisabled:
		return ObfuscationConfigMapMACDisabledName
	default:
		return ObfuscationConfigMapValidName
	}
}

// seedObfuscationConfigMap creates an obfuscation policy ConfigMap in the operator namespace.
func seedObfuscationConfigMap(fixture ObfuscateConfigMapFixture) {
	loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", fixture.filename()), operatorNamespace)
}

// deleteObfuscationConfigMap removes a seeded obfuscation ConfigMap from the operator namespace.
func deleteObfuscationConfigMap(fixture ObfuscateConfigMapFixture) {
	loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", fixture.filename()), operatorNamespace)
}

// createObfuscateMustGather creates a MustGather CR with obfuscation enabled by default.
func createObfuscateMustGather(name, namespace, serviceAccountName string, retainResources bool, opts *MustGatherCROptions) *mustgatherv1alpha1.MustGather {
	enabled := true
	if opts == nil {
		opts = &MustGatherCROptions{}
	}
	if opts.Obfuscate == nil {
		opts.Obfuscate = &ObfuscateOptions{Enabled: &enabled}
	} else if opts.Obfuscate.Enabled == nil {
		opts.Obfuscate.Enabled = &enabled
	}
	return createMustGatherCR(name, namespace, serviceAccountName, retainResources, opts)
}

func applyObfuscateSpec(mg *mustgatherv1alpha1.MustGather, ob *ObfuscateOptions) {
	if ob == nil {
		return
	}

	enabled := true
	if ob.Enabled != nil {
		enabled = *ob.Enabled
	}

	mg.Spec.Obfuscate = &mustgatherv1alpha1.ObfuscateConfig{
		Enabled: &enabled,
	}

	if ob.ConfigMapRefName != "" {
		mg.Spec.Obfuscate.ObfuscationConfigRef = &corev1.LocalObjectReference{
			Name: ob.ConfigMapRefName,
		}
	}

	if ob.SourcePVCName != "" {
		mg.Spec.Obfuscate.Source = &mustgatherv1alpha1.ObfuscateSourceConfig{
			Claim: mustgatherv1alpha1.PersistentVolumeClaimReference{
				Name: ob.SourcePVCName,
			},
			SubPath: ob.SourceSubPath,
		}
	}
}

func containerEnvMap(container *corev1.Container) map[string]string {
	env := make(map[string]string, len(container.Env))
	for _, e := range container.Env {
		env[e.Name] = e.Value
	}
	return env
}

func findJobContainer(job *batchv1.Job, containerName string) *corev1.Container {
	for i := range job.Spec.Template.Spec.Containers {
		if job.Spec.Template.Spec.Containers[i].Name == containerName {
			return &job.Spec.Template.Spec.Containers[i]
		}
	}
	return nil
}

func createCaseManagementSecret(name, namespace, username, password string) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"test": nonAdminLabel,
			},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"username": username,
			"password": password,
		},
	}
	err := nonAdminClient.Create(testCtx, secret)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred(), "Failed to create case management secret")
	}
}

// waitForMustGatherJobSuccess waits until the MustGather Job completes successfully and returns the Job and Pod.
func waitForMustGatherJobSuccess(mustGatherName, namespace string) (*batchv1.Job, *corev1.Pod) {
	job := &batchv1.Job{}
	var mustGatherPod *corev1.Pod

	ginkgo.By("Waiting for Job to be created")
	Eventually(func(g Gomega) {
		mg := &mustgatherv1alpha1.MustGather{}
		g.Expect(nonAdminClient.Get(testCtx, client.ObjectKey{
			Name:      mustGatherName,
			Namespace: namespace,
		}, mg)).To(Succeed())

		if mg.Status.Status == "Failed" {
			ginkgo.Fail(fmt.Sprintf("MustGather failed before Job creation: %s", mg.Status.Reason))
		}

		g.Expect(nonAdminClient.Get(testCtx, client.ObjectKey{
			Name:      mustGatherName,
			Namespace: namespace,
		}, job)).To(Succeed(), "Job should be created for MustGather")
	}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

	ginkgo.By("Waiting for Pod to be scheduled")
	Eventually(func(g Gomega) {
		podList := &corev1.PodList{}
		g.Expect(nonAdminClient.List(testCtx, podList,
			client.InNamespace(namespace),
			client.MatchingLabels{jobNameLabelKey: mustGatherName})).To(Succeed())
		g.Expect(podList.Items).NotTo(BeEmpty(), "Pod should be created by Job")
		mustGatherPod = &podList.Items[0]
	}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())

	ginkgo.By("Waiting for Job to complete successfully")
	Eventually(func() bool {
		if err := nonAdminClient.Get(testCtx, client.ObjectKey{
			Name:      mustGatherName,
			Namespace: namespace,
		}, job); err != nil {
			return false
		}
		if job.Status.Succeeded > 0 {
			return true
		}
		if job.Status.Failed > 0 {
			details := jobFailureDetails(namespace, mustGatherPod)
			ginkgo.Fail(fmt.Sprintf("Job failed: %s", details))
		}
		return false
	}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).Should(BeTrue(), "Job should complete successfully")

	if mustGatherPod != nil && mustGatherPod.Name != "" {
		Expect(nonAdminClient.Get(testCtx, client.ObjectKey{
			Name:      mustGatherPod.Name,
			Namespace: namespace,
		}, mustGatherPod)).To(Succeed())
	}

	return job, mustGatherPod
}

func jobFailureDetails(namespace string, mustGatherPod *corev1.Pod) string {
	var details []string
	if mustGatherPod != nil && mustGatherPod.Name != "" {
		tmpPod := &corev1.Pod{}
		if err := nonAdminClient.Get(testCtx, client.ObjectKey{
			Name:      mustGatherPod.Name,
			Namespace: namespace,
		}, tmpPod); err == nil {
			for _, cs := range tmpPod.Status.ContainerStatuses {
				if cs.State.Terminated != nil {
					details = append(details, fmt.Sprintf(
						"container[%s] exitCode=%d reason=%q message=%q",
						cs.Name, cs.State.Terminated.ExitCode, cs.State.Terminated.Reason, cs.State.Terminated.Message,
					))
				}
			}
		}
	}
	if len(details) == 0 {
		return "<no failure details available>"
	}
	return strings.Join(details, "; ")
}

func volumeMountForContainer(container *corev1.Container, volumeName string) *corev1.VolumeMount {
	for i := range container.VolumeMounts {
		if container.VolumeMounts[i].Name == volumeName {
			return &container.VolumeMounts[i]
		}
	}
	return nil
}

func assertObfuscateSourceModeJobShape(job *batchv1.Job, wantSourceSubPath string) {
	Expect(findJobContainer(job, gatherContainerName)).To(BeNil(),
		"source PVC obfuscation Job must omit gather container")

	upload := findJobContainer(job, uploadContainerName)
	Expect(upload).NotTo(BeNil(), "source PVC obfuscation Job must include upload container")

	uploadEnv := containerEnvMap(upload)
	Expect(uploadEnv[uploadEnvObfuscateName]).To(Equal("true"))

	sourceMount := volumeMountForContainer(upload, obfuscateSourceVolumeNameE2E)
	Expect(sourceMount).NotTo(BeNil(), "upload container should mount obfuscate-source volume")
	Expect(sourceMount.ReadOnly).To(BeTrue(), "source PVC mount must be read-only")
	if wantSourceSubPath != "" {
		Expect(sourceMount.SubPath).To(Equal(wantSourceSubPath))
	}
}

func seedObfuscateSourcePVC(namespace, pvcName, subPath string) {
	podName := fmt.Sprintf("seed-obfuscate-source-%d", time.Now().UnixNano())
	seedPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			RestartPolicy:      corev1.RestartPolicyNever,
			ServiceAccountName: serviceAccount,
			Containers: []corev1.Container{
				{
					Name:    "seed",
					Image:   operatorImage,
					Command: []string{"/bin/sh", "-c"},
					Args: []string{fmt.Sprintf(
						`set -eux
mkdir -p /source/%[1]s/cluster
echo "obfuscate-e2e-source-marker" > /source/%[1]s/%[2]s
echo "connection from 10.0.0.42 mac aa:bb:cc:dd:ee:ff" > /source/%[1]s/cluster/node.log
ls -laR /source/%[1]s`,
						subPath, sourcePVCMarkerFileName,
					)},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "source-pvc", MountPath: "/source"},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "source-pvc",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
						},
					},
				},
			},
		},
	}

	Expect(nonAdminClient.Create(testCtx, seedPod)).To(Succeed())

	Eventually(func() corev1.PodPhase {
		current := &corev1.Pod{}
		if err := nonAdminClient.Get(testCtx, client.ObjectKey{Name: podName, Namespace: namespace}, current); err != nil {
			return corev1.PodUnknown
		}
		return current.Status.Phase
	}).WithTimeout(3*time.Minute).WithPolling(5*time.Second).Should(
		Equal(corev1.PodSucceeded), "seed pod should populate source PVC bundle")

	_ = nonAdminClient.Delete(testCtx, seedPod)
}

func readSourcePVCMarker(namespace, pvcName, subPath string) string {
	podName := fmt.Sprintf("read-source-marker-%d", time.Now().UnixNano())
	readPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			RestartPolicy:      corev1.RestartPolicyNever,
			ServiceAccountName: serviceAccount,
			Containers: []corev1.Container{
				{
					Name:    "read",
					Image:   operatorImage,
					Command: []string{"/bin/sh", "-c"},
					Args: []string{fmt.Sprintf(
						`cat /source/%s/%s`,
						subPath, sourcePVCMarkerFileName,
					)},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "source-pvc", MountPath: "/source"},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "source-pvc",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
						},
					},
				},
			},
		},
	}

	Expect(nonAdminClient.Create(testCtx, readPod)).To(Succeed())

	Eventually(func() corev1.PodPhase {
		current := &corev1.Pod{}
		if err := nonAdminClient.Get(testCtx, client.ObjectKey{Name: podName, Namespace: namespace}, current); err != nil {
			return corev1.PodUnknown
		}
		return current.Status.Phase
	}).WithTimeout(2*time.Minute).WithPolling(5*time.Second).Should(
		Equal(corev1.PodSucceeded), "marker read pod should succeed")

	logs, err := getContainerLogs(namespace, podName, "read")
	Expect(err).NotTo(HaveOccurred(), "Failed to read source PVC marker")
	_ = nonAdminClient.Delete(testCtx, readPod)
	return strings.TrimSpace(logs)
}

func createObfuscateSourceMustGather(name, namespace string, retain bool, sourceSubPath string, upload *UploadTargetOptions) *mustgatherv1alpha1.MustGather {
	opts := &MustGatherCROptions{
		Obfuscate: &ObfuscateOptions{
			SourcePVCName: mustGatherPVCName,
			SourceSubPath: sourceSubPath,
		},
	}
	if upload != nil {
		opts.UploadTarget = upload
	}
	return createObfuscateMustGather(name, namespace, serviceAccount, retain, opts)
}

func createObfuscateCustomConfigMustGather(name, namespace string, retain bool, configMapName string) *mustgatherv1alpha1.MustGather {
	return createObfuscateMustGather(name, namespace, serviceAccount, retain, &MustGatherCROptions{
		Obfuscate: &ObfuscateOptions{
			ConfigMapRefName: configMapName,
			SourcePVCName:    mustGatherPVCName,
			SourceSubPath:    obfuscateSourceBundleSubPath,
		},
	})
}

func waitForObfuscationConfigInvalidStatus(mustGatherName, namespace, wantReason, wantMessageContains string) *mustgatherv1alpha1.MustGather {
	fetchedMG := &mustgatherv1alpha1.MustGather{}
	Eventually(func(g Gomega) {
		g.Expect(nonAdminClient.Get(testCtx, client.ObjectKey{
			Name:      mustGatherName,
			Namespace: namespace,
		}, fetchedMG)).To(Succeed())
		g.Expect(fetchedMG.Status.Status).To(Equal("Failed"),
			"MustGather should fail obfuscation config validation")
		cond := findStatusCondition(fetchedMG, conditionObfuscationConfigInvalidE2E)
		g.Expect(cond).NotTo(BeNil(), "expected ObfuscationConfigInvalid condition")
		g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		g.Expect(cond.Reason).To(Equal(wantReason))
		g.Expect(cond.Message).To(ContainSubstring(wantMessageContains))
		assertNoReconcileErrorCondition(fetchedMG)
	}).WithTimeout(2*time.Minute).WithPolling(5*time.Second).Should(Succeed())
	return fetchedMG
}

func findStatusCondition(mg *mustgatherv1alpha1.MustGather, conditionType string) *metav1.Condition {
	for i := range mg.Status.Conditions {
		if mg.Status.Conditions[i].Type == conditionType {
			return &mg.Status.Conditions[i]
		}
	}
	return nil
}

func assertNoReconcileErrorCondition(mg *mustgatherv1alpha1.MustGather) {
	for _, cond := range mg.Status.Conditions {
		Expect(cond.Type).NotTo(Equal("ReconcileError"),
			"obfuscation config failures must use distinct condition type, not ReconcileError")
	}
}

func assertMustGatherJobNotCreated(mustGatherName, namespace string) {
	job := &batchv1.Job{}
	Consistently(func() bool {
		err := nonAdminClient.Get(testCtx, client.ObjectKey{
			Name:      mustGatherName,
			Namespace: namespace,
		}, job)
		return apierrors.IsNotFound(err)
	}).WithTimeout(30*time.Second).WithPolling(5*time.Second).Should(BeTrue(),
		"Job should NOT be created when obfuscation ConfigMap validation fails")
}
