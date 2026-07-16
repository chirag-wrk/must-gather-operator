package mustgather

import (
	"fmt"
	"math"
	"path"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	mustgatherv1alpha1 "github.com/openshift/must-gather-operator/api/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const (
	// well-known dir for ca certificates to be mounted in a container,
	// canonical to `trustedCAMountPath`, de-coupled for test.
	wellKnownCADirForTest = "/etc/pki/tls/certs"
	// canonical to `outputVolumeName`, de-coupled for test.
	knownStorageVolumeMountNameForTest = "must-gather-output"
)

func Test_initializeJobTemplate(t *testing.T) {
	testName := "testName"
	testNamespace := "testNamespace"
	testServiceAccountRef := "testServiceAccountRef"
	pvcClaimName := "test-pvc"
	pvcSubPath := "test-path"

	tests := []struct {
		name               string
		storage            *mustgatherv1alpha1.Storage
		caConfigMap        string
		obfuscate          *mustgatherv1alpha1.ObfuscateConfig
		wantSourcePVC      bool
		wantSourceReadOnly bool
		wantSourceClaim    string
	}{
		{
			name: "Without PVC",
		},
		{
			name: "With PVC",
			storage: &mustgatherv1alpha1.Storage{
				Type: mustgatherv1alpha1.StorageTypePersistentVolume,
				PersistentVolume: mustgatherv1alpha1.PersistentVolumeConfig{
					Claim: mustgatherv1alpha1.PersistentVolumeClaimReference{
						Name: pvcClaimName,
					},
					SubPath: pvcSubPath,
				},
			},
		},
		{
			name:        "With CA config map",
			caConfigMap: "trusted-ca-cert-001",
		},
		{
			name: "With obfuscate config map",
			obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
				ObfuscationConfigRef: &v1.LocalObjectReference{Name: "custom-obfuscate-config"},
			},
		},
		{
			name: "With obfuscate source PVC",
			obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
				Enabled: ptr.To(true),
				Source: &mustgatherv1alpha1.ObfuscateSourceConfig{
					Claim: mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "source-pvc"},
				},
			},
			wantSourcePVC:      true,
			wantSourceReadOnly: true,
			wantSourceClaim:    "source-pvc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := initializeJobTemplate(testName, testNamespace, testServiceAccountRef, tt.storage, tt.caConfigMap, tt.obfuscate)

			if got := job.Name; got != testName {
				t.Fatalf("job name from initializeJobTemplate() was not correctly set. got %v, wanted %v", got, testName)
			}

			if got := job.Namespace; got != testNamespace {
				t.Fatalf("job namespace from initializeJobTemplate() was not correctly set. got %v, wanted %v", got, testNamespace)
			}

			if got := job.Spec.Template.Spec.ServiceAccountName; got != testServiceAccountRef {
				t.Fatalf("job service account name from initializeJobTemplate() was not correctly set. got %v, wanted %v", got, testServiceAccountRef)
			}

			if (tt.storage != nil || tt.caConfigMap != "" || tt.obfuscate != nil) && len(job.Spec.Template.Spec.Volumes) == 0 {
				t.Fatalf("expected at least one volume to be present")
			}

			foundStorageVolume := false
			foundCAVolume := false
			foundObfuscateConfigVolume := false
			obfuscateConfigMap := obfuscateConfigMapName(tt.obfuscate)
			for _, v := range job.Spec.Template.Spec.Volumes {
				if v.Name == knownStorageVolumeMountNameForTest {
					foundStorageVolume = true

					if tt.wantSourcePVC {
						if v.PersistentVolumeClaim.ClaimName != tt.wantSourceClaim {
							t.Fatalf("expected source pvc claim %q, got %q", tt.wantSourceClaim, v.PersistentVolumeClaim.ClaimName)
						}
						if v.PersistentVolumeClaim.ReadOnly != tt.wantSourceReadOnly {
							t.Fatalf("expected source pvc readOnly=%v, got %v", tt.wantSourceReadOnly, v.PersistentVolumeClaim.ReadOnly)
						}
					} else if tt.storage != nil && v.PersistentVolumeClaim.ClaimName != tt.storage.PersistentVolume.Claim.Name {
						t.Fatalf("pvc claim name from initializeJobTemplate() was not correctly set. got %v, wanted %v", v.PersistentVolumeClaim.ClaimName, tt.storage.PersistentVolume.Claim.Name)
					}
				}

				if v.ConfigMap != nil && v.ConfigMap.Name == tt.caConfigMap {
					foundCAVolume = true

					if v.ConfigMap.Name != tt.caConfigMap {
						t.Fatalf("config map CA from initializeJobTemplate() was not correctly set. got %v, wanted %v", v.ConfigMap.Name, tt.caConfigMap)
					}
				}

				if v.Name == obfuscateConfigVolumeName && v.ConfigMap != nil && v.ConfigMap.Name == obfuscateConfigMap {
					foundObfuscateConfigVolume = true
				}
			}

			if (tt.storage != nil || tt.wantSourcePVC) && !foundStorageVolume {
				t.Fatalf("expected volumeMount for storage was not found got %v", job.Spec.Template.Spec.Volumes)
			}

			if tt.caConfigMap != "" && !foundCAVolume {
				t.Fatalf("expected volumeMount for CA was not found got %v", job.Spec.Template.Spec.Volumes)
			}

			if obfuscateConfigMap != "" && !foundObfuscateConfigVolume {
				t.Fatalf("expected obfuscate config volume was not found got %v", job.Spec.Template.Spec.Volumes)
			}
		})
	}
}

func Test_getGatherContainer(t *testing.T) {
	testSinceTime := time.Date(2026, 1, 7, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name            string
		audit           bool
		timeout         time.Duration
		mustGatherImage string
		storage         *mustgatherv1alpha1.Storage
		command         []string
		args            []string
		caConfigMap     string
		timeFilter      *GatherTimeFilter
		obfuscate       *mustgatherv1alpha1.ObfuscateConfig
		wantChown       bool
	}{
		{
			name:            "no audit",
			timeout:         5 * time.Second,
			mustGatherImage: "quay.io/foo/bar/must-gather:latest",
		},
		{
			name:            "audit",
			audit:           true,
			timeout:         0 * time.Second,
			mustGatherImage: "quay.io/foo/bar/must-gather:latest",
		},
		{
			name:            "with trusted CA config map",
			timeout:         5 * time.Second,
			mustGatherImage: "quay.io/foo/bar/must-gather:latest",
			caConfigMap:     "trusted-ca-cert-001",
		},
		{
			name:    "with PVC",
			timeout: 5 * time.Second,
			storage: &mustgatherv1alpha1.Storage{
				Type: mustgatherv1alpha1.StorageTypePersistentVolume,
				PersistentVolume: mustgatherv1alpha1.PersistentVolumeConfig{
					Claim: mustgatherv1alpha1.PersistentVolumeClaimReference{
						Name: "test-pvc",
					},
					SubPath: "test-path",
				},
			},
		},
		{
			name:    "with PVC empty subPath sets subPathExpr to POD_NAME only",
			timeout: 5 * time.Second,
			storage: &mustgatherv1alpha1.Storage{
				Type: mustgatherv1alpha1.StorageTypePersistentVolume,
				PersistentVolume: mustgatherv1alpha1.PersistentVolumeConfig{
					Claim:   mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "test-pvc"},
					SubPath: "",
				},
			},
		},
		{
			name:    "with PVC whitespace subPath sets subPathExpr to POD_NAME only",
			timeout: 5 * time.Second,
			storage: &mustgatherv1alpha1.Storage{
				Type: mustgatherv1alpha1.StorageTypePersistentVolume,
				PersistentVolume: mustgatherv1alpha1.PersistentVolumeConfig{
					Claim:   mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "test-pvc"},
					SubPath: "   ",
				},
			},
		},
		{
			name:    "with PVC slash-only subPath sets subPathExpr to POD_NAME only",
			timeout: 5 * time.Second,
			storage: &mustgatherv1alpha1.Storage{
				Type: mustgatherv1alpha1.StorageTypePersistentVolume,
				PersistentVolume: mustgatherv1alpha1.PersistentVolumeConfig{
					Claim:   mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "test-pvc"},
					SubPath: "/",
				},
			},
		},
		{
			name:            "robust timeout",
			timeout:         1500 * time.Millisecond,
			mustGatherImage: "quay.io/foo/bar/must-gather:latest",
		},
		{
			name:            "custom command and args",
			timeout:         5 * time.Second,
			mustGatherImage: "quay.io/foo/bar/must-gather:latest",
			command:         []string{"/usr/bin/custom-gather"},
			args:            []string{"--verbose", "--subsystem=network"},
		},
		{
			name:            "with since duration",
			timeout:         5 * time.Second,
			mustGatherImage: "quay.io/foo/bar/must-gather:latest",
			timeFilter: &GatherTimeFilter{
				Since: 2 * time.Hour,
			},
		},
		{
			name:            "with sinceTime",
			timeout:         5 * time.Second,
			mustGatherImage: "quay.io/foo/bar/must-gather:latest",
			timeFilter: &GatherTimeFilter{
				SinceTime: &testSinceTime,
			},
		},
		{
			name:            "obfuscate enabled appends chown",
			timeout:         5 * time.Second,
			mustGatherImage: "quay.io/foo/bar/must-gather:latest",
			obfuscate:       &mustgatherv1alpha1.ObfuscateConfig{Enabled: ptr.To(true)},
			wantChown:       true,
		},
		{
			name:            "obfuscate disabled omits chown",
			timeout:         5 * time.Second,
			mustGatherImage: "quay.io/foo/bar/must-gather:latest",
			obfuscate:       &mustgatherv1alpha1.ObfuscateConfig{Enabled: ptr.To(false)},
		},
		{
			name:            "obfuscate enabled with source omits chown",
			timeout:         5 * time.Second,
			mustGatherImage: "quay.io/foo/bar/must-gather:latest",
			obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
				Enabled: ptr.To(true),
				Source: &mustgatherv1alpha1.ObfuscateSourceConfig{
					Claim: mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "bundle-pvc"},
				},
			},
		},
		{
			name:            "custom command omits chown even when obfuscate enabled",
			timeout:         5 * time.Second,
			mustGatherImage: "quay.io/foo/bar/must-gather:latest",
			command:         []string{"/usr/bin/custom-gather"},
			obfuscate:       &mustgatherv1alpha1.ObfuscateConfig{Enabled: ptr.To(true)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			container := getGatherContainer(tt.mustGatherImage, tt.audit, tt.timeout, tt.storage, tt.caConfigMap, tt.timeFilter, tt.command, tt.args, tt.obfuscate)

			if len(tt.command) == 0 {
				containerCommand := container.Command[2]
				if tt.audit && !strings.Contains(containerCommand, gatherCommandBinaryAudit) {
					t.Fatalf("gather container command expected with binary %v but it wasn't present", gatherCommandBinaryAudit)
				} else if !tt.audit && !strings.Contains(containerCommand, gatherCommandBinaryNoAudit) {
					t.Fatalf("gather container command expected with binary %v but it wasn't present", gatherCommandBinaryNoAudit)
				}
				timeoutInSeconds := int(math.Ceil(tt.timeout.Seconds()))
				if !strings.HasPrefix(containerCommand, fmt.Sprintf("timeout %d", timeoutInSeconds)) {
					t.Fatalf("the duration was not properly added to the container command, got %v but wanted %v", strings.Split(containerCommand, " ")[1], timeoutInSeconds)
				}
				hasChown := strings.Contains(containerCommand, obfuscateChownSuffix)
				if tt.wantChown && !hasChown {
					t.Fatalf("expected gather command to include chown suffix %q", obfuscateChownSuffix)
				}
				if !tt.wantChown && hasChown {
					t.Fatalf("expected gather command without chown suffix")
				}
			} else {
				if !reflect.DeepEqual(container.Command, tt.command) {
					t.Fatalf("expected container command %v but got %v", tt.command, container.Command)
				}
				if !reflect.DeepEqual(container.Args, tt.args) {
					t.Fatalf("expected container args %v but got %v", tt.args, container.Args)
				}
			}

			if container.Image != tt.mustGatherImage {
				t.Fatalf("expected container image %v but got %v", tt.mustGatherImage, container.Image)
			}

			// Check trusted CA configmap volume mount behavior
			foundTrustedCAMount := false
			for _, vm := range container.VolumeMounts {
				if vm.Name == trustedCAVolumeName {
					foundTrustedCAMount = true
					if vm.MountPath != wellKnownCADirForTest {
						t.Fatalf("trusted CA volume mount path was not correctly set. got %v, wanted %v", vm.MountPath, wellKnownCADirForTest)
					}
					if !vm.ReadOnly {
						t.Fatalf("trusted CA volume mount expected to be read-only")
					}
				}
			}
			if tt.caConfigMap != "" && !foundTrustedCAMount {
				t.Fatalf("expected trusted CA volume mount to be present when caConfigMap is provided")
			}
			if tt.caConfigMap == "" && foundTrustedCAMount {
				t.Fatalf("did not expect trusted CA volume mount when caConfigMap is empty")
			}

			if tt.storage != nil {
				if len(container.VolumeMounts) == 0 {
					t.Fatalf("expected at least one volume mount when storage is provided")
				}
				volumeMount := container.VolumeMounts[0]
				if volumeMount.Name != outputVolumeName {
					t.Fatalf("volume mount name was not correctly set. got %v, wanted %v", volumeMount.Name, outputVolumeName)
				}
				base := strings.Trim(strings.TrimSpace(tt.storage.PersistentVolume.SubPath), "/")
				wantExpr := path.Join(base, fmt.Sprintf("$(%s)", podNameEnvVar))
				if volumeMount.SubPathExpr != wantExpr {
					t.Fatalf("volume mount subPathExpr was not correctly set. got %q, wanted %q", volumeMount.SubPathExpr, wantExpr)
				}
				if volumeMount.SubPath != "" {
					t.Fatalf("did not expect volume mount subPath to be set when using subPathExpr, got %q", volumeMount.SubPath)
				}
			}

			// POD_NAME env var should be present only when SubPathExpr is used.
			hasPodNameEnv := false
			for _, env := range container.Env {
				if env.Name == podNameEnvVar {
					hasPodNameEnv = true
					if env.ValueFrom == nil || env.ValueFrom.FieldRef == nil || env.ValueFrom.FieldRef.FieldPath != "metadata.name" {
						t.Fatalf("expected %s env var to be sourced from metadata.name via fieldRef, got %#v", podNameEnvVar, env)
					}
				}
			}
			// SubPathExpr is always set for PVC storage (for per-pod isolation), so POD_NAME env is always present.
			hasPVCStorage := tt.storage != nil && tt.storage.Type == mustgatherv1alpha1.StorageTypePersistentVolume
			if hasPVCStorage && !hasPodNameEnv {
				t.Fatalf("expected %s env var when PVC storage is used (SubPathExpr is set)", podNameEnvVar)
			}
			if !hasPVCStorage && hasPodNameEnv {
				t.Fatalf("did not expect %s env var when storage is not PVC", podNameEnvVar)
			}

			// Check time filter environment variables
			if tt.timeFilter != nil {
				envMap := envValues(container)
				if tt.timeFilter.Since > 0 {
					if envMap[gatherEnvSince] != tt.timeFilter.Since.String() {
						t.Fatalf("expected %s env var to be %v, got %v", gatherEnvSince, tt.timeFilter.Since.String(), envMap[gatherEnvSince])
					}
				}
				if tt.timeFilter.SinceTime != nil {
					expectedTime := tt.timeFilter.SinceTime.Format(time.RFC3339)
					if envMap[gatherEnvSinceTime] != expectedTime {
						t.Fatalf("expected %s env var to be %v, got %v", gatherEnvSinceTime, expectedTime, envMap[gatherEnvSinceTime])
					}
				}
			}
		})
	}
}

func Test_getUploadContainer(t *testing.T) {
	tests := []struct {
		name             string
		operatorImage    string
		caseId           string
		host             string
		internalUser     bool
		storage          *mustgatherv1alpha1.Storage
		httpProxy        string
		httpsProxy       string
		noProxy          string
		mountCAConfigMap bool
		secretKeyRefName v1.LocalObjectReference
		obfuscate        *mustgatherv1alpha1.ObfuscateConfig
	}{
		{
			name:             "All fields present",
			operatorImage:    "testImage",
			caseId:           "1234",
			host:             "sftp.example.com",
			internalUser:     true,
			httpProxy:        "testHttpProxy",
			httpsProxy:       "testHttpsProxy",
			noProxy:          "testNoProxy",
			secretKeyRefName: v1.LocalObjectReference{Name: "testSecretKeyRefName"},
		},
		{
			name:             "Non-internal user",
			operatorImage:    "testImage",
			caseId:           "1234",
			httpProxy:        "testHttpProxy",
			httpsProxy:       "testHttpsProxy",
			noProxy:          "testNoProxy",
			secretKeyRefName: v1.LocalObjectReference{Name: "testSecretKeyRefName"},
		},
		{
			name:             "No http proxy envar",
			operatorImage:    "testImage",
			caseId:           "1234",
			httpsProxy:       "testHttpsProxy",
			noProxy:          "testNoProxy",
			secretKeyRefName: v1.LocalObjectReference{Name: "testSecretKeyRefName"},
		},
		{
			name:             "No https proxy envar",
			operatorImage:    "testImage",
			caseId:           "1234",
			httpProxy:        "testHttpProxy",
			noProxy:          "testNoProxy",
			secretKeyRefName: v1.LocalObjectReference{Name: "testSecretKeyRefName"},
		},
		{
			name:             "No noproxy envar",
			operatorImage:    "testImage",
			caseId:           "1234",
			httpProxy:        "testHttpProxy",
			httpsProxy:       "testHttpsProxy",
			secretKeyRefName: v1.LocalObjectReference{Name: "testSecretKeyRefName"},
		},
		{
			name:             "With trusted CA config map",
			operatorImage:    "testImage",
			caseId:           "1234",
			httpProxy:        "testHttpProxy",
			httpsProxy:       "testHttpsProxy",
			secretKeyRefName: v1.LocalObjectReference{Name: "testSecretKeyRefName"},
			mountCAConfigMap: true,
		},
		{
			name:          "With PVC subPath",
			operatorImage: "testImage",
			caseId:        "1234",
			secretKeyRefName: v1.LocalObjectReference{
				Name: "testSecretKeyRefName",
			},
			storage: &mustgatherv1alpha1.Storage{
				Type: mustgatherv1alpha1.StorageTypePersistentVolume,
				PersistentVolume: mustgatherv1alpha1.PersistentVolumeConfig{
					Claim: mustgatherv1alpha1.PersistentVolumeClaimReference{
						Name: "test-pvc",
					},
					SubPath: "test-path",
				},
			},
		},
		{
			name:          "With PVC empty subPath sets subPathExpr to POD_NAME only",
			operatorImage: "testImage",
			caseId:        "1234",
			secretKeyRefName: v1.LocalObjectReference{
				Name: "testSecretKeyRefName",
			},
			storage: &mustgatherv1alpha1.Storage{
				Type: mustgatherv1alpha1.StorageTypePersistentVolume,
				PersistentVolume: mustgatherv1alpha1.PersistentVolumeConfig{
					Claim:   mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "test-pvc"},
					SubPath: "",
				},
			},
		},
		{
			name:          "With PVC whitespace subPath sets subPathExpr to POD_NAME only",
			operatorImage: "testImage",
			caseId:        "1234",
			secretKeyRefName: v1.LocalObjectReference{
				Name: "testSecretKeyRefName",
			},
			storage: &mustgatherv1alpha1.Storage{
				Type: mustgatherv1alpha1.StorageTypePersistentVolume,
				PersistentVolume: mustgatherv1alpha1.PersistentVolumeConfig{
					Claim:   mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "test-pvc"},
					SubPath: "   ",
				},
			},
		},
		{
			name:          "With PVC slash-only subPath sets subPathExpr to POD_NAME only",
			operatorImage: "testImage",
			caseId:        "1234",
			secretKeyRefName: v1.LocalObjectReference{
				Name: "testSecretKeyRefName",
			},
			storage: &mustgatherv1alpha1.Storage{
				Type: mustgatherv1alpha1.StorageTypePersistentVolume,
				PersistentVolume: mustgatherv1alpha1.PersistentVolumeConfig{
					Claim:   mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "test-pvc"},
					SubPath: "/",
				},
			},
		},
		{
			name:          "Obfuscate-only without SFTP credentials",
			operatorImage: "testImage",
		},
		{
			name:          "Obfuscate enabled sets obfuscate env",
			operatorImage: "testImage",
			obfuscate:     &mustgatherv1alpha1.ObfuscateConfig{Enabled: ptr.To(true)},
		},
		{
			name:          "Obfuscate config ref mounts ConfigMap and sets obfuscate_config env",
			operatorImage: "testImage",
			obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
				Enabled: ptr.To(true),
				ObfuscationConfigRef: &v1.LocalObjectReference{
					Name: "custom-obfuscate-config",
				},
			},
		},
		{
			name:          "Obfuscate source uses direct upload and read-only PVC mount",
			operatorImage: "testImage",
			obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
				Enabled: ptr.To(true),
				Source: &mustgatherv1alpha1.ObfuscateSourceConfig{
					Claim:   mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "source-pvc"},
					SubPath: "must-gather-2026-06-25",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFailed := false
			var sftp *uploadSFTPParams
			if tt.caseId != "" && tt.secretKeyRefName.Name != "" {
				sftp = &uploadSFTPParams{
					caseID:       tt.caseId,
					host:         tt.host,
					internalUser: tt.internalUser,
					secretRef:    tt.secretKeyRefName,
				}
			}
			container := getUploadContainer(tt.operatorImage, tt.storage, tt.httpProxy, tt.httpsProxy, tt.noProxy, tt.mountCAConfigMap, sftp, tt.obfuscate)

			if container.Image != tt.operatorImage {
				t.Fatalf("expected container image %v but got %v", tt.operatorImage, container.Image)
			}

			if tt.mountCAConfigMap {
				mountedCAExists := false
				for _, vm := range container.VolumeMounts {
					if vm.MountPath == wellKnownCADirForTest {
						mountedCAExists = true
					}
				}

				if !mountedCAExists {
					t.Fatalf("expected a CA cert volumeMount in upload container")
				}
			}

			if tt.storage != nil && tt.storage.Type == mustgatherv1alpha1.StorageTypePersistentVolume {
				var outputMount *v1.VolumeMount
				for i := range container.VolumeMounts {
					if container.VolumeMounts[i].Name == outputVolumeName {
						outputMount = &container.VolumeMounts[i]
						break
					}
				}
				if outputMount == nil {
					t.Fatalf("expected output volume mount %q to be present", outputVolumeName)
				}
				base := strings.Trim(strings.TrimSpace(tt.storage.PersistentVolume.SubPath), "/")
				wantExpr := path.Join(base, fmt.Sprintf("$(%s)", podNameEnvVar))
				if outputMount.SubPathExpr != wantExpr {
					t.Fatalf("expected output volume mount subPathExpr %q but got %q", wantExpr, outputMount.SubPathExpr)
				}
				if outputMount.SubPath != "" {
					t.Fatalf("did not expect output volume mount subPath to be set when using subPathExpr, got %q", outputMount.SubPath)
				}
			}

			// POD_NAME env var is present when SubPathExpr is used (always for PVC storage).
			hasPodNameEnv := false
			for _, env := range container.Env {
				if env.Name == podNameEnvVar {
					hasPodNameEnv = true
					if env.ValueFrom == nil || env.ValueFrom.FieldRef == nil || env.ValueFrom.FieldRef.FieldPath != "metadata.name" {
						t.Fatalf("expected %s env var to be sourced from metadata.name via fieldRef, got %#v", podNameEnvVar, env)
					}
				}
			}
			hasPVCStorage := tt.storage != nil && tt.storage.Type == mustgatherv1alpha1.StorageTypePersistentVolume
			if hasPVCStorage && !hasPodNameEnv {
				t.Fatalf("expected %s env var when PVC storage is used (SubPathExpr is set)", podNameEnvVar)
			}
			if !hasPVCStorage && hasPodNameEnv {
				t.Fatalf("did not expect %s env var when storage is not PVC", podNameEnvVar)
			}

			for _, env := range container.Env {
				switch env.Name {
				case uploadEnvCaseId:
					if sftp == nil {
						t.Fatalf("did not expect %s env var without SFTP params", uploadEnvCaseId)
					}
					if env.Value != tt.caseId {
						t.Fatalf("expected case ID envar %v but got %v", tt.caseId, env.Value)
					}
				case uploadEnvHost:
					if sftp == nil {
						t.Fatalf("did not expect %s env var without SFTP params", uploadEnvHost)
					}
					if env.Value != tt.host {
						t.Fatalf("expected host envar %v but got %v", tt.host, env.Value)
					}
				case uploadEnvInternalUser:
					if sftp == nil {
						t.Fatalf("did not expect %s env var without SFTP params", uploadEnvInternalUser)
					}
					if env.Value != strconv.FormatBool(tt.internalUser) {
						t.Fatalf("expected internal user envar %v but got %v", tt.internalUser, env.Value)
					}
				case uploadEnvHttpProxy:
					if env.Value != tt.httpProxy {
						t.Fatalf("expected httpproxy envar %v but got %v", tt.httpProxy, env.Value)
					}
				case uploadEnvHttpsProxy:
					if env.Value != tt.httpsProxy {
						t.Fatalf("expected httpsproxy envar %v but got %v", tt.httpsProxy, env.Value)
					}
				case uploadEnvNoProxy:
					if env.Value != tt.noProxy {
						t.Fatalf("expected noproxy envar %v but got %v", tt.noProxy, env.Value)
					}
				case uploadEnvUsername, uploadEnvPassword:
					if sftp == nil {
						t.Fatalf("did not expect %s env var without SFTP params", env.Name)
					}
					if !reflect.DeepEqual(env.ValueFrom.SecretKeyRef.LocalObjectReference, tt.secretKeyRefName) {
						t.Fatalf("expected %v envar to have secret key ref name %v but got %v", env.Name, tt.secretKeyRefName.Name, env.ValueFrom.SecretKeyRef.Name)
					}
				}

				if testFailed {
					t.Error()
				}
			}

			if sftp == nil {
				for _, name := range []string{uploadEnvCaseId, uploadEnvHost, uploadEnvInternalUser, uploadEnvUsername, uploadEnvPassword} {
					if _, ok := envValues(container)[name]; ok {
						t.Fatalf("did not expect %s env var for obfuscate-only upload container", name)
					}
				}
				if envValues(container)[uploadEnvMustGatherOutput] != volumeMountPath {
					t.Fatalf("expected %s=%s", uploadEnvMustGatherOutput, volumeMountPath)
				}
				if envValues(container)[uploadEnvMustGatherUpload] != volumeUploadMountPath {
					t.Fatalf("expected %s=%s", uploadEnvMustGatherUpload, volumeUploadMountPath)
				}
			}

			if tt.obfuscate != nil && tt.obfuscate.Enabled != nil && *tt.obfuscate.Enabled {
				if envValues(container)[obfuscateEnvEnabled] != "true" {
					t.Fatalf("expected %s=true when obfuscation enabled", obfuscateEnvEnabled)
				}
			} else if _, ok := envValues(container)[obfuscateEnvEnabled]; ok {
				t.Fatalf("did not expect %s env var when obfuscation disabled", obfuscateEnvEnabled)
			}

			if tt.obfuscate != nil && tt.obfuscate.ObfuscationConfigRef != nil && tt.obfuscate.ObfuscationConfigRef.Name != "" {
				if envValues(container)[obfuscateEnvConfig] != obfuscateConfigMountPath {
					t.Fatalf("expected %s=%s", obfuscateEnvConfig, obfuscateConfigMountPath)
				}
				var configMount *v1.VolumeMount
				for i := range container.VolumeMounts {
					if container.VolumeMounts[i].Name == obfuscateConfigVolumeName {
						configMount = &container.VolumeMounts[i]
						break
					}
				}
				if configMount == nil {
					t.Fatalf("expected obfuscate config volume mount %q", obfuscateConfigVolumeName)
				}
				if configMount.MountPath != obfuscateConfigMountPath {
					t.Fatalf("expected mount path %q, got %q", obfuscateConfigMountPath, configMount.MountPath)
				}
				if configMount.SubPath != obfuscateConfigMapKey {
					t.Fatalf("expected subPath %q, got %q", obfuscateConfigMapKey, configMount.SubPath)
				}
				if !configMount.ReadOnly {
					t.Fatalf("expected obfuscate config mount to be read-only")
				}
			}

			if tt.obfuscate != nil && tt.obfuscate.Source != nil && tt.obfuscate.Source.Claim.Name != "" {
				uploadCmd := container.Command[2]
				if strings.Contains(uploadCmd, "pgrep") {
					t.Fatalf("did not expect pgrep in upload command for source PVC mode")
				}
				if !strings.Contains(uploadCmd, uploadCommandDirect) {
					t.Fatalf("expected direct upload command for source PVC mode")
				}
				var outputMount *v1.VolumeMount
				for i := range container.VolumeMounts {
					if container.VolumeMounts[i].Name == outputVolumeName {
						outputMount = &container.VolumeMounts[i]
						break
					}
				}
				if outputMount == nil {
					t.Fatalf("expected output volume mount %q", outputVolumeName)
				}
				if !outputMount.ReadOnly {
					t.Fatalf("expected source PVC mount to be read-only")
				}
				if outputMount.SubPath != strings.Trim(tt.obfuscate.Source.SubPath, "/") {
					t.Fatalf("expected subPath %q, got %q", tt.obfuscate.Source.SubPath, outputMount.SubPath)
				}
				if outputMount.SubPathExpr != "" {
					t.Fatalf("did not expect subPathExpr for source PVC mode")
				}
			}
		})
	}
}

func Test_getJobTemplate_UploadContainerGating(t *testing.T) {
	t.Setenv(DefaultMustGatherImageEnv, "quay.io/foo/bar/must-gather:latest")

	enabled := true
	disabled := false

	tests := []struct {
		name       string
		spec       mustgatherv1alpha1.MustGatherSpec
		wantUpload bool
		wantSFTP   bool
	}{
		{
			name: "obfuscate enabled with source only includes upload without SFTP env",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
				Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
					Enabled: &enabled,
					Source: &mustgatherv1alpha1.ObfuscateSourceConfig{
						Claim: mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "source-pvc"},
					},
				},
			},
			wantUpload: true,
			wantSFTP:   false,
		},
		{
			name: "obfuscate enabled with uploadTarget includes upload with SFTP env",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
				Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
					Enabled: &enabled,
				},
				UploadTarget: &mustgatherv1alpha1.UploadTargetSpec{
					Type: mustgatherv1alpha1.UploadTypeSFTP,
					SFTP: &mustgatherv1alpha1.SFTPSpec{
						CaseID: "5678",
						Host:   "sftp.example.com",
						CaseManagementAccountSecretRef: v1.LocalObjectReference{
							Name: "case-secret",
						},
					},
				},
			},
			wantUpload: true,
			wantSFTP:   true,
		},
		{
			name: "obfuscate nil and no uploadTarget omits upload container",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
			},
			wantUpload: false,
		},
		{
			name: "obfuscate disabled and no uploadTarget omits upload container",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
				Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
					Enabled: &disabled,
				},
			},
			wantUpload: false,
		},
		{
			name: "uploadTarget only preserves backward-compatible SFTP upload",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
				UploadTarget: &mustgatherv1alpha1.UploadTargetSpec{
					Type: mustgatherv1alpha1.UploadTypeSFTP,
					SFTP: &mustgatherv1alpha1.SFTPSpec{
						CaseID: "9012",
						CaseManagementAccountSecretRef: v1.LocalObjectReference{
							Name: "legacy-secret",
						},
					},
				},
			},
			wantUpload: true,
			wantSFTP:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mg := mustgatherv1alpha1.MustGather{
				ObjectMeta: metav1.ObjectMeta{Name: "mg", Namespace: "ns"},
				Spec:       tt.spec,
			}

			job := getJobTemplate("image", "operator-image", mg, "")
			upload, ok := uploadContainerInJob(job)
			if ok != tt.wantUpload {
				t.Fatalf("expected upload container present=%v, got %v", tt.wantUpload, ok)
			}
			if !tt.wantUpload {
				return
			}

			uploadEnv := envValues(upload)
			if tt.wantSFTP {
				if uploadEnv[uploadEnvCaseId] == "" {
					t.Fatalf("expected non-empty %s env var", uploadEnvCaseId)
				}
				if !hasSecretEnv(upload, uploadEnvUsername) || !hasSecretEnv(upload, uploadEnvPassword) {
					t.Fatalf("expected username/password secret env vars when SFTP upload is configured")
				}
			} else {
				sftpLiteralEnv := []string{uploadEnvCaseId, uploadEnvHost, uploadEnvInternalUser}
				for _, name := range sftpLiteralEnv {
					if _, has := uploadEnv[name]; has {
						t.Fatalf("did not expect %s env var without SFTP upload target", name)
					}
				}
				if hasSecretEnv(upload, uploadEnvUsername) || hasSecretEnv(upload, uploadEnvPassword) {
					t.Fatalf("did not expect username/password secret env vars without SFTP upload target")
				}
			}
		})
	}
}

func Test_getJobTemplate_SourcePVCMode(t *testing.T) {
	t.Setenv(DefaultMustGatherImageEnv, "quay.io/foo/bar/must-gather:latest")

	enabled := true
	tests := []struct {
		name          string
		spec          mustgatherv1alpha1.MustGatherSpec
		wantUpload    bool
		wantSourcePVC string
		wantSubPath   string
	}{
		{
			name: "obfuscate-only source mode omits gather and uses source PVC",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
				Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
					Enabled: &enabled,
					Source: &mustgatherv1alpha1.ObfuscateSourceConfig{
						Claim: mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "source-pvc"},
					},
				},
			},
			wantUpload:    true,
			wantSourcePVC: "source-pvc",
		},
		{
			name: "obfuscate source with subPath and upload target",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
				Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
					Enabled: &enabled,
					Source: &mustgatherv1alpha1.ObfuscateSourceConfig{
						Claim:   mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "bundle-pvc"},
						SubPath: "must-gather-2026-06-25",
					},
				},
				UploadTarget: &mustgatherv1alpha1.UploadTargetSpec{
					Type: mustgatherv1alpha1.UploadTypeSFTP,
					SFTP: &mustgatherv1alpha1.SFTPSpec{
						CaseID: "1234",
						CaseManagementAccountSecretRef: v1.LocalObjectReference{Name: "case-secret"},
					},
				},
			},
			wantUpload:    true,
			wantSourcePVC: "bundle-pvc",
			wantSubPath:   "must-gather-2026-06-25",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mg := mustgatherv1alpha1.MustGather{
				ObjectMeta: metav1.ObjectMeta{Name: "mg", Namespace: "ns"},
				Spec:       tt.spec,
			}

			job := getJobTemplate("image", "operator-image", mg, "")
			if len(job.Spec.Template.Spec.Containers) != 1 {
				t.Fatalf("expected exactly one container in source mode, got %d", len(job.Spec.Template.Spec.Containers))
			}

			_, hasGather := gatherContainerInJob(job)
			if hasGather {
				t.Fatalf("did not expect gather container in source PVC mode")
			}

			upload, ok := uploadContainerInJob(job)
			if ok != tt.wantUpload {
				t.Fatalf("expected upload container present=%v, got %v", tt.wantUpload, ok)
			}

			uploadCmd := upload.Command[2]
			if strings.Contains(uploadCmd, "pgrep") {
				t.Fatalf("did not expect pgrep in upload command for source PVC mode")
			}
			if !strings.Contains(uploadCmd, uploadCommandDirect) {
				t.Fatalf("expected direct upload command for source PVC mode")
			}

			var outputVolume *v1.Volume
			for i := range job.Spec.Template.Spec.Volumes {
				if job.Spec.Template.Spec.Volumes[i].Name == outputVolumeName {
					outputVolume = &job.Spec.Template.Spec.Volumes[i]
					break
				}
			}
			if outputVolume == nil || outputVolume.PersistentVolumeClaim == nil {
				t.Fatalf("expected output volume to use source PVC")
			}
			if outputVolume.PersistentVolumeClaim.ClaimName != tt.wantSourcePVC {
				t.Fatalf("expected source PVC %q, got %q", tt.wantSourcePVC, outputVolume.PersistentVolumeClaim.ClaimName)
			}
			if !outputVolume.PersistentVolumeClaim.ReadOnly {
				t.Fatalf("expected source PVC volume to be read-only")
			}

			var outputMount *v1.VolumeMount
			for i := range upload.VolumeMounts {
				if upload.VolumeMounts[i].Name == outputVolumeName {
					outputMount = &upload.VolumeMounts[i]
					break
				}
			}
			if outputMount == nil {
				t.Fatalf("expected upload container output mount")
			}
			if !outputMount.ReadOnly {
				t.Fatalf("expected upload source mount to be read-only")
			}
			if tt.wantSubPath != "" && outputMount.SubPath != tt.wantSubPath {
				t.Fatalf("expected subPath %q, got %q", tt.wantSubPath, outputMount.SubPath)
			}
		})
	}
}

func Test_getJobTemplate_ObfuscateUploadContract(t *testing.T) {
	t.Setenv(DefaultMustGatherImageEnv, "quay.io/foo/bar/must-gather:latest")

	enabled := true
	mg := mustgatherv1alpha1.MustGather{
		ObjectMeta: metav1.ObjectMeta{Name: "mg", Namespace: "ns"},
		Spec: mustgatherv1alpha1.MustGatherSpec{
			ServiceAccountName: "default",
			Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
				Enabled: &enabled,
				ObfuscationConfigRef: &v1.LocalObjectReference{
					Name: "custom-obfuscate-config",
				},
			},
		},
	}

	job := getJobTemplate("image", "operator-image", mg, "")

	upload, ok := uploadContainerInJob(job)
	if !ok {
		t.Fatalf("expected upload container when obfuscation enabled")
	}
	uploadEnv := envValues(upload)
	if uploadEnv[obfuscateEnvEnabled] != "true" {
		t.Fatalf("expected %s=true, got %q", obfuscateEnvEnabled, uploadEnv[obfuscateEnvEnabled])
	}
	if uploadEnv[obfuscateEnvConfig] != obfuscateConfigMountPath {
		t.Fatalf("expected %s=%s, got %q", obfuscateEnvConfig, obfuscateConfigMountPath, uploadEnv[obfuscateEnvConfig])
	}

	var configVolume *v1.Volume
	for i := range job.Spec.Template.Spec.Volumes {
		if job.Spec.Template.Spec.Volumes[i].Name == obfuscateConfigVolumeName {
			configVolume = &job.Spec.Template.Spec.Volumes[i]
			break
		}
	}
	if configVolume == nil {
		t.Fatalf("expected job volume %q", obfuscateConfigVolumeName)
	}
	if configVolume.ConfigMap == nil || configVolume.ConfigMap.Name != "custom-obfuscate-config" {
		t.Fatalf("expected obfuscate config volume to reference custom-obfuscate-config ConfigMap")
	}
}

func Test_getJobTemplate_ObfuscationModes(t *testing.T) {
	t.Setenv(DefaultMustGatherImageEnv, "quay.io/foo/bar/must-gather:latest")

	enabled := true
	disabled := false

	tests := []struct {
		name              string
		spec              mustgatherv1alpha1.MustGatherSpec
		wantGather        bool
		wantChown         bool
		wantUpload        bool
		wantObfuscateEnv  bool
		wantSFTP          bool
		wantPgrep         bool
		wantSourcePVC     string
		wantSourceSubPath string
		wantConfigMount   bool
		wantConfigVolume  string
	}{
		{
			name: "mode 1 gather obfuscate and upload",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
				Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
					Enabled: &enabled,
				},
				UploadTarget: &mustgatherv1alpha1.UploadTargetSpec{
					Type: mustgatherv1alpha1.UploadTypeSFTP,
					SFTP: &mustgatherv1alpha1.SFTPSpec{
						CaseID: "case-1",
						CaseManagementAccountSecretRef: v1.LocalObjectReference{Name: "case-secret"},
					},
				},
			},
			wantGather:       true,
			wantChown:        true,
			wantUpload:       true,
			wantObfuscateEnv: true,
			wantSFTP:         true,
			wantPgrep:        true,
		},
		{
			name: "mode 1 with custom obfuscation config ref",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
				Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
					Enabled: &enabled,
					ObfuscationConfigRef: &v1.LocalObjectReference{
						Name: "my-obfuscate-config",
					},
				},
				UploadTarget: &mustgatherv1alpha1.UploadTargetSpec{
					Type: mustgatherv1alpha1.UploadTypeSFTP,
					SFTP: &mustgatherv1alpha1.SFTPSpec{
						CaseID: "case-2",
						CaseManagementAccountSecretRef: v1.LocalObjectReference{Name: "case-secret"},
					},
				},
			},
			wantGather:       true,
			wantChown:        true,
			wantUpload:       true,
			wantObfuscateEnv: true,
			wantSFTP:         true,
			wantPgrep:        true,
			wantConfigMount:  true,
			wantConfigVolume: "my-obfuscate-config",
		},
		{
			name: "mode 2 obfuscate-only from source PVC",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
				Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
					Enabled: &enabled,
					Source: &mustgatherv1alpha1.ObfuscateSourceConfig{
						Claim: mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "source-pvc"},
					},
				},
			},
			wantUpload:       true,
			wantObfuscateEnv: true,
			wantSourcePVC:    "source-pvc",
		},
		{
			name: "mode 3 obfuscate upload from source PVC",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
				Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
					Enabled: &enabled,
					Source: &mustgatherv1alpha1.ObfuscateSourceConfig{
						Claim:   mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "bundle-pvc"},
						SubPath: "must-gather-2026-06-25",
					},
				},
				UploadTarget: &mustgatherv1alpha1.UploadTargetSpec{
					Type: mustgatherv1alpha1.UploadTypeSFTP,
					SFTP: &mustgatherv1alpha1.SFTPSpec{
						CaseID: "case-3",
						CaseManagementAccountSecretRef: v1.LocalObjectReference{Name: "case-secret"},
					},
				},
			},
			wantUpload:        true,
			wantObfuscateEnv:  true,
			wantSFTP:          true,
			wantSourcePVC:     "bundle-pvc",
			wantSourceSubPath: "must-gather-2026-06-25",
		},
		{
			name: "negative obfuscate disabled preserves legacy upload gating",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
				Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
					Enabled: &disabled,
				},
				UploadTarget: &mustgatherv1alpha1.UploadTargetSpec{
					Type: mustgatherv1alpha1.UploadTypeSFTP,
					SFTP: &mustgatherv1alpha1.SFTPSpec{
						CaseID: "legacy-case",
						CaseManagementAccountSecretRef: v1.LocalObjectReference{Name: "legacy-secret"},
					},
				},
			},
			wantGather: true,
			wantUpload: true,
			wantSFTP:   true,
			wantPgrep:  true,
		},
		{
			name: "negative no obfuscate and no uploadTarget",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
			},
			wantGather: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mg := mustgatherv1alpha1.MustGather{
				ObjectMeta: metav1.ObjectMeta{Name: "mg", Namespace: "ns"},
				Spec:       tt.spec,
			}

			job := getJobTemplate("image", "operator-image", mg, "")

			gather, hasGather := gatherContainerInJob(job)
			if hasGather != tt.wantGather {
				t.Fatalf("expected gather container present=%v, got %v", tt.wantGather, hasGather)
			}
			if tt.wantGather {
				gatherCmd := gather.Command[2]
				hasChown := strings.Contains(gatherCmd, obfuscateChownSuffix)
				if hasChown != tt.wantChown {
					t.Fatalf("expected chown in gather command=%v, got %v", tt.wantChown, hasChown)
				}
			}

			upload, hasUpload := uploadContainerInJob(job)
			if hasUpload != tt.wantUpload {
				t.Fatalf("expected upload container present=%v, got %v", tt.wantUpload, hasUpload)
			}
			if !tt.wantUpload {
				return
			}

			uploadEnv := envValues(upload)
			if tt.wantObfuscateEnv {
				if uploadEnv[obfuscateEnvEnabled] != "true" {
					t.Fatalf("expected %s=true", obfuscateEnvEnabled)
				}
			} else if _, ok := uploadEnv[obfuscateEnvEnabled]; ok {
				t.Fatalf("did not expect %s env var", obfuscateEnvEnabled)
			}

			uploadCmd := upload.Command[2]
			hasPgrep := strings.Contains(uploadCmd, "pgrep")
			if hasPgrep != tt.wantPgrep {
				t.Fatalf("expected pgrep in upload command=%v, got %v", tt.wantPgrep, hasPgrep)
			}

			if tt.wantSFTP {
				if uploadEnv[uploadEnvCaseId] == "" {
					t.Fatalf("expected SFTP case ID env var")
				}
				if !hasSecretEnv(upload, uploadEnvUsername) {
					t.Fatalf("expected SFTP username secret env var")
				}
			} else if hasSecretEnv(upload, uploadEnvUsername) {
				t.Fatalf("did not expect SFTP credential env vars")
			}

			if tt.wantSourcePVC != "" {
				if hasGather {
					t.Fatalf("did not expect gather container when source PVC is set")
				}
				var outputVolume *v1.Volume
				for i := range job.Spec.Template.Spec.Volumes {
					if job.Spec.Template.Spec.Volumes[i].Name == outputVolumeName {
						outputVolume = &job.Spec.Template.Spec.Volumes[i]
						break
					}
				}
				if outputVolume == nil || outputVolume.PersistentVolumeClaim == nil {
					t.Fatalf("expected source PVC on output volume")
				}
				if outputVolume.PersistentVolumeClaim.ClaimName != tt.wantSourcePVC {
					t.Fatalf("expected source PVC %q, got %q", tt.wantSourcePVC, outputVolume.PersistentVolumeClaim.ClaimName)
				}
				if !outputVolume.PersistentVolumeClaim.ReadOnly {
					t.Fatalf("expected source PVC volume to be read-only")
				}

				var outputMount *v1.VolumeMount
				for i := range upload.VolumeMounts {
					if upload.VolumeMounts[i].Name == outputVolumeName {
						outputMount = &upload.VolumeMounts[i]
						break
					}
				}
				if outputMount == nil || !outputMount.ReadOnly {
					t.Fatalf("expected read-only source PVC mount on upload container")
				}
				if tt.wantSourceSubPath != "" && outputMount.SubPath != tt.wantSourceSubPath {
					t.Fatalf("expected source subPath %q, got %q", tt.wantSourceSubPath, outputMount.SubPath)
				}
			}

			if tt.wantConfigMount {
				if uploadEnv[obfuscateEnvConfig] != obfuscateConfigMountPath {
					t.Fatalf("expected %s=%s", obfuscateEnvConfig, obfuscateConfigMountPath)
				}
				var configMount *v1.VolumeMount
				for i := range upload.VolumeMounts {
					if upload.VolumeMounts[i].Name == obfuscateConfigVolumeName {
						configMount = &upload.VolumeMounts[i]
						break
					}
				}
				if configMount == nil {
					t.Fatalf("expected obfuscate config volume mount on upload container")
				}
				if configMount.MountPath != obfuscateConfigMountPath {
					t.Fatalf("expected config mount path %q, got %q", obfuscateConfigMountPath, configMount.MountPath)
				}
				if configMount.SubPath != obfuscateConfigMapKey {
					t.Fatalf("expected config subPath %q, got %q", obfuscateConfigMapKey, configMount.SubPath)
				}
			}

			if tt.wantConfigVolume != "" {
				var configVolume *v1.Volume
				for i := range job.Spec.Template.Spec.Volumes {
					if job.Spec.Template.Spec.Volumes[i].Name == obfuscateConfigVolumeName {
						configVolume = &job.Spec.Template.Spec.Volumes[i]
						break
					}
				}
				if configVolume == nil || configVolume.ConfigMap == nil {
					t.Fatalf("expected obfuscate config volume in job spec")
				}
				if configVolume.ConfigMap.Name != tt.wantConfigVolume {
					t.Fatalf("expected config volume %q, got %q", tt.wantConfigVolume, configVolume.ConfigMap.Name)
				}
			}
		})
	}
}

func Test_getJobTemplate_GatherSpec_BuildsTimeFilter(t *testing.T) {
	t.Setenv(DefaultMustGatherImageEnv, "quay.io/foo/bar/must-gather:latest")

	sinceTime := metav1.NewTime(time.Date(2026, 1, 7, 10, 11, 12, 0, time.UTC))

	tests := []struct {
		name        string
		gatherSpec  *mustgatherv1alpha1.GatherSpec
		wantSince   string
		wantSinceTs string
	}{
		{
			name: "no gatherSpec means no time filter env vars",
		},
		{
			name:       "gatherSpec with since builds timeFilter.Since",
			gatherSpec: &mustgatherv1alpha1.GatherSpec{Since: &metav1.Duration{Duration: 2 * time.Hour}},
			wantSince:  "2h0m0s",
		},
		{
			name:        "gatherSpec with sinceTime builds timeFilter.SinceTime",
			gatherSpec:  &mustgatherv1alpha1.GatherSpec{SinceTime: &sinceTime},
			wantSinceTs: "2026-01-07T10:11:12Z",
		},
		{
			name:       "gatherSpec present but with no since/sinceTime means no time filter env vars",
			gatherSpec: &mustgatherv1alpha1.GatherSpec{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mg := mustgatherv1alpha1.MustGather{
				ObjectMeta: metav1.ObjectMeta{Name: "mg", Namespace: "ns"},
				Spec: mustgatherv1alpha1.MustGatherSpec{
					ServiceAccountName: "default",
					GatherSpec:         tt.gatherSpec,
				},
			}

			job := getJobTemplate("img", "operator-image", mg, "")
			gather := findGatherContainerInJob(t, job)
			got := envValues(gather)

			if tt.wantSince == "" {
				if _, ok := got[gatherEnvSince]; ok {
					t.Fatalf("did not expect %s env var, got %v", gatherEnvSince, got[gatherEnvSince])
				}
			} else if got[gatherEnvSince] != tt.wantSince {
				t.Fatalf("expected %s=%s, got %s", gatherEnvSince, tt.wantSince, got[gatherEnvSince])
			}

			if tt.wantSinceTs == "" {
				if _, ok := got[gatherEnvSinceTime]; ok {
					t.Fatalf("did not expect %s env var, got %v", gatherEnvSinceTime, got[gatherEnvSinceTime])
				}
			} else if got[gatherEnvSinceTime] != tt.wantSinceTs {
				t.Fatalf("expected %s=%s, got %s", gatherEnvSinceTime, tt.wantSinceTs, got[gatherEnvSinceTime])
			}
		})
	}
}

func Test_getJobTemplate_ProxyAuditTimeout(t *testing.T) {
	t.Setenv(DefaultMustGatherImageEnv, "quay.io/foo/bar/must-gather:latest")

	timeout := metav1.Duration{Duration: 5 * time.Second}

	tests := []struct {
		name        string
		audit       bool
		timeout     *metav1.Duration
		httpProxy   string
		httpsProxy  string
		noProxy     string
		wantAudit   bool
		wantTimeout string
		wantProxies bool
	}{
		{
			name:        "audit false and nil timeout default; no proxy env vars",
			wantAudit:   false,
			wantTimeout: "timeout 0",
			wantProxies: false,
		},
		{
			name:        "audit true and timeout set; proxy env vars propagate to upload container",
			audit:       true,
			timeout:     &timeout,
			httpProxy:   "http://proxy.example:8080",
			httpsProxy:  "https://proxy.example:8443",
			noProxy:     "127.0.0.1,localhost,.cluster.local",
			wantAudit:   true,
			wantTimeout: "timeout 5",
			wantProxies: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Always set proxy vars per test case to avoid leakage from host env.
			t.Setenv("HTTP_PROXY", tt.httpProxy)
			t.Setenv("HTTPS_PROXY", tt.httpsProxy)
			t.Setenv("NO_PROXY", tt.noProxy)

			mg := mustgatherv1alpha1.MustGather{
				ObjectMeta: metav1.ObjectMeta{Name: "mg", Namespace: "ns"},
				Spec: mustgatherv1alpha1.MustGatherSpec{
					ServiceAccountName: "default",
					MustGatherTimeout:  tt.timeout,
					GatherSpec: &mustgatherv1alpha1.GatherSpec{
						Audit: tt.audit,
					},
					UploadTarget: &mustgatherv1alpha1.UploadTargetSpec{
						Type: mustgatherv1alpha1.UploadTypeSFTP,
						SFTP: &mustgatherv1alpha1.SFTPSpec{
							CaseID: "1234",
							Host:   "sftp.example.com",
							CaseManagementAccountSecretRef: v1.LocalObjectReference{
								Name: "case-mgmt-secret",
							},
						},
					},
				},
			}

			job := getJobTemplate("image", "operator-image", mg, "")

			gather := findGatherContainerInJob(t, job)
			gatherCmd := gather.Command[2]
			if tt.wantAudit {
				if !strings.Contains(gatherCmd, gatherCommandBinaryAudit) {
					t.Fatalf("expected gather command to contain %v but got %v", gatherCommandBinaryAudit, gatherCmd)
				}
			} else {
				if !strings.Contains(gatherCmd, gatherCommandBinaryNoAudit) {
					t.Fatalf("expected gather command to contain %v but got %v", gatherCommandBinaryNoAudit, gatherCmd)
				}
			}
			if !strings.HasPrefix(gatherCmd, tt.wantTimeout) {
				t.Fatalf("expected gather command to start with %q but got %q", tt.wantTimeout, gatherCmd)
			}

			upload := findUploadContainerInJob(t, job)
			uploadEnv := envValues(upload)
			if tt.wantProxies {
				if uploadEnv[uploadEnvHttpProxy] != tt.httpProxy {
					t.Fatalf("expected %s=%v, got %v", uploadEnvHttpProxy, tt.httpProxy, uploadEnv[uploadEnvHttpProxy])
				}
				if uploadEnv[uploadEnvHttpsProxy] != tt.httpsProxy {
					t.Fatalf("expected %s=%v, got %v", uploadEnvHttpsProxy, tt.httpsProxy, uploadEnv[uploadEnvHttpsProxy])
				}
				if uploadEnv[uploadEnvNoProxy] != tt.noProxy {
					t.Fatalf("expected %s=%v, got %v", uploadEnvNoProxy, tt.noProxy, uploadEnv[uploadEnvNoProxy])
				}
			} else {
				if _, ok := uploadEnv[uploadEnvHttpProxy]; ok {
					t.Fatalf("did not expect %s env var, got %v", uploadEnvHttpProxy, uploadEnv[uploadEnvHttpProxy])
				}
				if _, ok := uploadEnv[uploadEnvHttpsProxy]; ok {
					t.Fatalf("did not expect %s env var, got %v", uploadEnvHttpsProxy, uploadEnv[uploadEnvHttpsProxy])
				}
				if _, ok := uploadEnv[uploadEnvNoProxy]; ok {
					t.Fatalf("did not expect %s env var, got %v", uploadEnvNoProxy, uploadEnv[uploadEnvNoProxy])
				}
			}
		})
	}
}

// helper to find gather container in a job
func findGatherContainerInJob(t *testing.T, job *batchv1.Job) v1.Container {
	t.Helper()
	for _, c := range job.Spec.Template.Spec.Containers {
		if c.Name == gatherContainerName {
			return c
		}
	}
	t.Fatalf("gather container not found in job")
	return v1.Container{}
}

// helper to find gather container in a job, if present
func gatherContainerInJob(job *batchv1.Job) (v1.Container, bool) {
	for _, c := range job.Spec.Template.Spec.Containers {
		if c.Name == gatherContainerName {
			return c, true
		}
	}
	return v1.Container{}, false
}

// helper to find upload container in a job, if present
func uploadContainerInJob(job *batchv1.Job) (v1.Container, bool) {
	for _, c := range job.Spec.Template.Spec.Containers {
		if c.Name == uploadContainerName {
			return c, true
		}
	}
	return v1.Container{}, false
}

func hasSecretEnv(container v1.Container, name string) bool {
	for _, env := range container.Env {
		if env.Name == name && env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
			return true
		}
	}
	return false
}

// helper to find upload container in a job
func findUploadContainerInJob(t *testing.T, job *batchv1.Job) v1.Container {
	t.Helper()
	for _, c := range job.Spec.Template.Spec.Containers {
		if c.Name == uploadContainerName {
			return c
		}
	}
	t.Fatalf("upload container not found in job")
	return v1.Container{}
}

// helper to map env name->value
func envValues(container v1.Container) map[string]string {
	m := make(map[string]string)
	for _, e := range container.Env {
		m[e.Name] = e.Value
	}
	return m
}
