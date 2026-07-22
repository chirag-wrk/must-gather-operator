package mustgather

import (
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"

	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/must-gather-operator/api/v1alpha1"
	"github.com/openshift/must-gather-operator/pkg/obfuscate"

	"github.com/operator-framework/operator-lib/proxy"
)

const (
	infraNodeLabelKey     = "node-role.kubernetes.io/infra"
	outputVolumeName      = "must-gather-output"
	uploadVolumeName      = "must-gather-upload"
	obfuscateSourceVolumeName = "obfuscate-source"
	trustedCAVolumeName   = "trusted-ca"
	volumeMountPath       = "/must-gather"
	volumeUploadMountPath = "/must-gather-upload"
	trustedCAMountPath    = "/etc/pki/tls/certs"

	gatherCommandBinaryAudit   = "gather_audit_logs"
	gatherCommandBinaryNoAudit = "gather"
	gatherCommand              = "timeout %v bash -x -c -- '/usr/bin/%v' 2>&1 | tee /must-gather/must-gather.log\n\nstatus=$?\nif [[ $status -eq 124 || $status -eq 137 ]]; then\n  echo \"Gather timed out.\"\n  exit 0\nfi | tee -a /must-gather/must-gather.log"
	gatherObfuscateChownSuffix = "\nchown -R 65534:65534 /must-gather"
	gatherContainerName        = "gather"

	// Environment variables for time-based log filtering
	gatherEnvSince     = "MUST_GATHER_SINCE"
	gatherEnvSinceTime = "MUST_GATHER_SINCE_TIME"

	backoffLimit              = 3
	uploadContainerName       = "upload"
	uploadEnvUsername         = "username"
	uploadEnvPassword         = "password"
	uploadEnvCaseId           = "caseid"
	uploadEnvHost             = "host"
	uploadEnvInternalUser     = "internal_user"
	uploadEnvHttpProxy        = "http_proxy"
	uploadEnvHttpsProxy       = "https_proxy"
	uploadEnvNoProxy          = "no_proxy"
	uploadEnvMustGatherOutput = "must_gather_output"
	uploadEnvMustGatherUpload = "must_gather_upload"
	uploadEnvFilenamePrefix   = "FILENAME_PREFIX"
	uploadEnvObfuscate        = "obfuscate"
	uploadEnvObfuscateConfig  = "obfuscate_config"

	obfuscateConfigVolumeName = "obfuscate-config"
	obfuscateConfigMountDir   = "/etc/must-gather-clean"
	obfuscateConfigMapKey     = "config.yaml"
	uploadCommand             = "count=0\nuntil [ $count -gt 4 ]\ndo\n  while `pgrep -a gather > /dev/null`\n  do\n    echo \"waiting for gathers to complete ...\"\n    sleep 120\n    count=0\n  done\n  echo \"no gather is running ($count / 4)\"\n  ((count++))\n  sleep 30\ndone\n/usr/local/bin/upload"

	// SSH directory and known hosts file
	sshDir         = "/tmp/must-gather-operator/.ssh"
	knownHostsFile = "/tmp/must-gather-operator/.ssh/known_hosts"
)

// obfuscateEnabled reports whether obfuscation is explicitly enabled on the MustGather spec.
func obfuscateEnabled(spec *v1alpha1.ObfuscateConfig) bool {
	return spec != nil && spec.Enabled != nil && *spec.Enabled
}

// customObfuscateConfigPath returns the mounted path for a user-supplied obfuscation ConfigMap.
func customObfuscateConfigPath() string {
	return path.Join(obfuscateConfigMountDir, obfuscateConfigMapKey)
}

func obfuscateConfigEnvPath(ref *corev1.LocalObjectReference) string {
	if ref != nil && ref.Name != "" {
		return customObfuscateConfigPath()
	}
	return obfuscate.DefaultObfuscateConfigPath
}

func outputSubPath(storage *v1alpha1.Storage, directoryName string) (string, bool) {
	if storage == nil || storage.Type != v1alpha1.StorageTypePersistentVolume {
		return "", false
	}

	base := strings.TrimSpace(storage.PersistentVolume.SubPath)
	base = strings.Trim(base, "/")

	return path.Join(base, directoryName), true
}

// sourceSubPath returns the sanitized subPath for mounting an obfuscate.source PVC.
// The second return value is false when no subPath should be applied (PVC root mount).
func sourceSubPath(source *v1alpha1.ObfuscateSourceConfig) (string, bool) {
	if source == nil {
		return "", false
	}

	subPath := strings.TrimSpace(source.SubPath)
	subPath = strings.Trim(subPath, "/")
	if subPath == "" {
		return "", false
	}

	return subPath, true
}

// GatherTimeFilter holds the time-based filtering options for log collection
type GatherTimeFilter struct {
	// Since is a relative duration (e.g., "2h", "30m")
	Since time.Duration
	// SinceTime is an absolute timestamp
	SinceTime *time.Time
}

func getJobTemplate(image string, operatorImage string, mustGather v1alpha1.MustGather, trustedCAConfigMapName string, directoryName string, operatorNamespace string) *batchv1.Job {
	obfuscate := mustGather.Spec.Obfuscate
	obfuscateOn := obfuscateEnabled(obfuscate)
	sourceMode := obfuscateOn && obfuscate != nil && obfuscate.Source != nil

	job := initializeJobTemplate(mustGather.Name, mustGather.Namespace, mustGather.Spec.ServiceAccountName, mustGather.Spec.Storage, trustedCAConfigMapName)

	if sourceMode {
		job.Spec.Template.Spec.Volumes = append(job.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: obfuscateSourceVolumeName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: obfuscate.Source.Claim.Name,
					ReadOnly:  true,
				},
			},
		})
	}

	if obfuscateOn && obfuscate.ObfuscationConfigRef != nil && obfuscate.ObfuscationConfigRef.Name != "" {
		// ConfigMaps referenced from operatorNamespace are mounted by name in the Job pod
		// namespace; cross-namespace copy is handled outside the template when required.
		_ = operatorNamespace
		job.Spec.Template.Spec.Volumes = append(job.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: obfuscateConfigVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: obfuscate.ObfuscationConfigRef.Name,
					},
					Items: []corev1.KeyToPath{
						{
							Key:  obfuscateConfigMapKey,
							Path: obfuscateConfigMapKey,
						},
					},
				},
			},
		})
	}

	var httpProxy, httpsProxy, noProxy string

	// Use operator's environment proxy variables
	envVars := proxy.ReadProxyVarsFromEnv()
	// the below loop should implicitly handle len(envVars) > 0
	for _, envVar := range envVars {
		switch envVar.Name {
		case "HTTP_PROXY":
			httpProxy = envVar.Value
		case "HTTPS_PROXY":
			httpsProxy = envVar.Value
		case "NO_PROXY":
			noProxy = envVar.Value
		}
	}

	if !sourceMode {
		var audit bool
		if mustGather.Spec.GatherSpec != nil {
			audit = mustGather.Spec.GatherSpec.Audit
		}

		timeout := time.Duration(0)
		if mustGather.Spec.MustGatherTimeout != nil {
			timeout = mustGather.Spec.MustGatherTimeout.Duration
		}

		// Build time filter from spec
		var timeFilter *GatherTimeFilter
		var command, args []string
		if mustGather.Spec.GatherSpec != nil {
			command = mustGather.Spec.GatherSpec.Command
			args = mustGather.Spec.GatherSpec.Args
			if mustGather.Spec.GatherSpec.Since != nil || mustGather.Spec.GatherSpec.SinceTime != nil {
				timeFilter = &GatherTimeFilter{}
				if mustGather.Spec.GatherSpec.Since != nil {
					timeFilter.Since = mustGather.Spec.GatherSpec.Since.Duration
				}
				if mustGather.Spec.GatherSpec.SinceTime != nil {
					t := mustGather.Spec.GatherSpec.SinceTime.Time
					timeFilter.SinceTime = &t
				}
			}
		}

		job.Spec.Template.Spec.Containers = append(
			job.Spec.Template.Spec.Containers,
			getGatherContainer(image, audit, timeout, mustGather.Spec.Storage, trustedCAConfigMapName, timeFilter, command, args, directoryName, obfuscateOn),
		)
	}

	var obfuscateConfigRef *corev1.LocalObjectReference
	if obfuscateOn && obfuscate.ObfuscationConfigRef != nil && obfuscate.ObfuscationConfigRef.Name != "" {
		obfuscateConfigRef = obfuscate.ObfuscationConfigRef
	}

	var source *v1alpha1.ObfuscateSourceConfig
	if sourceMode {
		source = obfuscate.Source
	}

	if shouldAddUploadContainer(mustGather) {
		caseID, host, internalUser, secretRef, hasSFTP := sftpUploadParams(mustGather)
		var sftpSecret *corev1.LocalObjectReference
		if hasSFTP {
			sftpSecret = secretRef
		}

		job.Spec.Template.Spec.Containers = append(
			job.Spec.Template.Spec.Containers,
			getUploadContainer(
				operatorImage,
				mustGather.Spec.Storage,
				directoryName,
				source,
				obfuscateConfigRef,
				obfuscateOn,
				caseID,
				host,
				internalUser,
				sftpSecret,
				httpProxy,
				httpsProxy,
				noProxy,
				trustedCAConfigMapName != "",
			),
		)
	}

	return job
}

func shouldAddUploadContainer(mustGather v1alpha1.MustGather) bool {
	if obfuscateEnabled(mustGather.Spec.Obfuscate) {
		return true
	}

	caseID, _, _, secretRef, ok := sftpUploadParams(mustGather)
	return ok && caseID != "" && secretRef != nil && secretRef.Name != ""
}

func sftpUploadParams(mustGather v1alpha1.MustGather) (caseID, host string, internalUser bool, secretRef *corev1.LocalObjectReference, ok bool) {
	if mustGather.Spec.UploadTarget == nil || mustGather.Spec.UploadTarget.Type != v1alpha1.UploadTypeSFTP {
		return "", "", false, nil, false
	}

	s := mustGather.Spec.UploadTarget.SFTP
	if s == nil || s.CaseID == "" || s.CaseManagementAccountSecretRef.Name == "" {
		return "", "", false, nil, false
	}

	return s.CaseID, s.Host, s.InternalUser, &s.CaseManagementAccountSecretRef, true
}

func initializeJobTemplate(name string, namespace string, serviceAccountRef string, storage *v1alpha1.Storage, trustedCAConfigMapName string) *batchv1.Job {
	outputVolume := corev1.Volume{
		Name:         outputVolumeName,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}

	if storage != nil && storage.Type == v1alpha1.StorageTypePersistentVolume {
		outputVolume.VolumeSource = corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: storage.PersistentVolume.Claim.Name,
			},
		}
	}

	volumes := []corev1.Volume{
		outputVolume,
		{
			Name:         uploadVolumeName,
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		},
	}

	// Add trusted CA volume if configmap name is provided
	if trustedCAConfigMapName != "" {
		volumes = append(volumes, corev1.Volume{
			Name: trustedCAVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: trustedCAConfigMapName,
					},
				},
			},
		})
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: ToPtr(int32(backoffLimit)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
								{
									Preference: corev1.NodeSelectorTerm{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      infraNodeLabelKey,
												Operator: corev1.NodeSelectorOpExists,
											},
										},
									},
									Weight: 1,
								},
							},
						},
					},
					Tolerations: []corev1.Toleration{
						{
							Effect:   corev1.TaintEffectNoSchedule,
							Key:      infraNodeLabelKey,
							Operator: corev1.TolerationOpExists,
						},
					},
					RestartPolicy:         corev1.RestartPolicyNever,
					ShareProcessNamespace: ToPtr(true),
					Volumes:               volumes,
					ServiceAccountName:    serviceAccountRef,
				},
			},
		},
	}
}

func getGatherContainer(image string, audit bool, timeout time.Duration, storage *v1alpha1.Storage, trustedCAConfigMapName string, timeFilter *GatherTimeFilter, command []string, args []string, directoryName string, obfuscateOn bool) corev1.Container {
	var commandBinary string
	if audit {
		commandBinary = gatherCommandBinaryAudit
	} else {
		commandBinary = gatherCommandBinaryNoAudit
	}

	volumeMount := corev1.VolumeMount{
		MountPath: volumeMountPath,
		Name:      outputVolumeName,
	}

	subPath, hasPVC := outputSubPath(storage, directoryName)
	if hasPVC {
		volumeMount.SubPath = subPath
	}

	volumeMounts := []corev1.VolumeMount{volumeMount}

	// Add trusted CA mount if configmap name is provided
	if trustedCAConfigMapName != "" {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      trustedCAVolumeName,
			MountPath: trustedCAMountPath,
			ReadOnly:  true,
		})
	}

	container := corev1.Container{
		Image:        image,
		Name:         gatherContainerName,
		VolumeMounts: volumeMounts,
	}

	if len(command) > 0 {
		container.Command = command
	} else {
		gatherScript := gatherCommand
		if obfuscateOn {
			gatherScript += gatherObfuscateChownSuffix
		}
		container.Command = []string{
			"/bin/bash",
			"-c",
			fmt.Sprintf(gatherScript, math.Ceil(timeout.Seconds()), commandBinary),
		}
	}

	if len(args) > 0 {
		container.Args = args
	}

	// Add time filter environment variables if specified
	if timeFilter != nil {
		if timeFilter.Since > 0 {
			container.Env = append(container.Env, corev1.EnvVar{
				Name:  gatherEnvSince,
				Value: timeFilter.Since.String(),
			})
		}
		if timeFilter.SinceTime != nil {
			container.Env = append(container.Env, corev1.EnvVar{
				Name:  gatherEnvSinceTime,
				Value: timeFilter.SinceTime.Format(time.RFC3339),
			})
		}
	}

	return container
}

func getUploadContainer(
	operatorImage string,
	storage *v1alpha1.Storage,
	directoryName string,
	source *v1alpha1.ObfuscateSourceConfig,
	obfuscationConfigRef *corev1.LocalObjectReference,
	obfuscateOn bool,
	caseID string,
	host string,
	internalUser bool,
	secretKeyRefName *corev1.LocalObjectReference,
	httpProxy string,
	httpsProxy string,
	noProxy string,
	shouldMountTrustedCAConfigMap bool,
) corev1.Container {
	uploadShellCommand := uploadCommand
	if secretKeyRefName != nil {
		uploadShellCommand = fmt.Sprintf("mkdir -p %s; touch %s; chmod 700 %s; chmod 600 %s; %s",
			sshDir, knownHostsFile, sshDir, knownHostsFile, uploadCommand)
	}

	var outputMount corev1.VolumeMount
	if source != nil {
		outputMount = corev1.VolumeMount{
			MountPath: volumeMountPath,
			Name:      obfuscateSourceVolumeName,
			ReadOnly:  true,
		}
		if subPath, hasSubPath := sourceSubPath(source); hasSubPath {
			outputMount.SubPath = subPath
		}
	} else {
		outputMount = corev1.VolumeMount{
			MountPath: volumeMountPath,
			Name:      outputVolumeName,
		}
		subPath, hasPVC := outputSubPath(storage, directoryName)
		if hasPVC {
			outputMount.SubPath = subPath
		}
	}

	volumeMounts := []corev1.VolumeMount{
		outputMount,
		{
			MountPath: volumeUploadMountPath,
			Name:      uploadVolumeName,
		},
	}

	if obfuscationConfigRef != nil {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      obfuscateConfigVolumeName,
			MountPath: obfuscateConfigMountDir,
			ReadOnly:  true,
		})
	}

	if shouldMountTrustedCAConfigMap {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      trustedCAVolumeName,
			MountPath: trustedCAMountPath,
			ReadOnly:  true,
		})
	}

	container := corev1.Container{
		Command: []string{
			"/bin/bash",
			"-c",
			uploadShellCommand,
		},
		Image:        operatorImage,
		Name:         uploadContainerName,
		VolumeMounts: volumeMounts,
	}

	if secretKeyRefName != nil {
		container.Env = []corev1.EnvVar{
			{
				Name: uploadEnvUsername,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key:                  uploadEnvUsername,
						LocalObjectReference: *secretKeyRefName,
					},
				},
			},
			{
				Name: uploadEnvPassword,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key:                  uploadEnvPassword,
						LocalObjectReference: *secretKeyRefName,
					},
				},
			},
			{
				Name:  uploadEnvCaseId,
				Value: caseID,
			},
			{
				Name:  uploadEnvHost,
				Value: host,
			},
			{
				Name:  uploadEnvMustGatherOutput,
				Value: volumeMountPath,
			},
			{
				Name:  uploadEnvMustGatherUpload,
				Value: volumeUploadMountPath,
			},
			{
				Name:  uploadEnvInternalUser,
				Value: strconv.FormatBool(internalUser),
			},
		}
	} else {
		container.Env = []corev1.EnvVar{
			{
				Name:  uploadEnvMustGatherOutput,
				Value: volumeMountPath,
			},
			{
				Name:  uploadEnvMustGatherUpload,
				Value: volumeUploadMountPath,
			},
		}
	}

	if httpProxy != "" {
		container.Env = append(container.Env, corev1.EnvVar{Name: uploadEnvHttpProxy, Value: httpProxy})
	}
	if httpsProxy != "" {
		container.Env = append(container.Env, corev1.EnvVar{Name: uploadEnvHttpsProxy, Value: httpsProxy})
	}
	if noProxy != "" {
		container.Env = append(container.Env, corev1.EnvVar{Name: uploadEnvNoProxy, Value: noProxy})
	}

	container.Env = append(container.Env, corev1.EnvVar{
		Name:  uploadEnvFilenamePrefix,
		Value: directoryName,
	})

	if obfuscateOn {
		container.Env = append(container.Env,
			corev1.EnvVar{Name: uploadEnvObfuscate, Value: "true"},
			corev1.EnvVar{Name: uploadEnvObfuscateConfig, Value: obfuscateConfigEnvPath(obfuscationConfigRef)},
		)
	}

	return container
}

func ToPtr[T any](t T) *T { return &t }
