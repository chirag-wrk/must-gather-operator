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
		name        string
		storage     *mustgatherv1alpha1.Storage
		caConfigMap string
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := initializeJobTemplate(testName, testNamespace, testServiceAccountRef, tt.storage, tt.caConfigMap)

			if got := job.Name; got != testName {
				t.Fatalf("job name from initializeJobTemplate() was not correctly set. got %v, wanted %v", got, testName)
			}

			if got := job.Namespace; got != testNamespace {
				t.Fatalf("job namespace from initializeJobTemplate() was not correctly set. got %v, wanted %v", got, testNamespace)
			}

			if got := job.Spec.Template.Spec.ServiceAccountName; got != testServiceAccountRef {
				t.Fatalf("job service account name from initializeJobTemplate() was not correctly set. got %v, wanted %v", got, testServiceAccountRef)
			}

			if (tt.storage != nil || tt.caConfigMap != "") && len(job.Spec.Template.Spec.Volumes) == 0 {
				t.Fatalf("expected at least one volume to be present")
			}

			foundStorageVolume := false
			foundCAVolume := false
			for _, v := range job.Spec.Template.Spec.Volumes {
				if v.Name == knownStorageVolumeMountNameForTest {
					foundStorageVolume = true

					if tt.storage != nil && v.PersistentVolumeClaim.ClaimName != tt.storage.PersistentVolume.Claim.Name {
						t.Fatalf("pvc claim name from initializeJobTemplate() was not correctly set. got %v, wanted %v", v.PersistentVolumeClaim.ClaimName, tt.storage.PersistentVolume.Claim.Name)
					}
				}

				if v.ConfigMap != nil && v.ConfigMap.Name == tt.caConfigMap {
					foundCAVolume = true

					if v.ConfigMap.Name != tt.caConfigMap {
						t.Fatalf("config map CA from initializeJobTemplate() was not correctly set. got %v, wanted %v", v.ConfigMap.Name, tt.caConfigMap)
					}
				}
			}

			if tt.storage != nil && !foundStorageVolume {
				t.Fatalf("expected volumeMount for storage was not found got %v", job.Spec.Template.Spec.Volumes)
			}

			if tt.caConfigMap != "" && !foundCAVolume {
				t.Fatalf("expected volumeMount for CA was not found got %v", job.Spec.Template.Spec.Volumes)
			}
		})
	}
}

func Test_getGatherContainer(t *testing.T) {
	testSinceTime := time.Date(2026, 1, 7, 10, 0, 0, 0, time.UTC)
	testDirName := "must-gather.local.456789abcdef.20260617T143025Z.042315"

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
		directoryName   string
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
			name:    "with PVC and directory name",
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
			directoryName: testDirName,
		},
		{
			name:    "with PVC empty subPath uses directory name only",
			timeout: 5 * time.Second,
			storage: &mustgatherv1alpha1.Storage{
				Type: mustgatherv1alpha1.StorageTypePersistentVolume,
				PersistentVolume: mustgatherv1alpha1.PersistentVolumeConfig{
					Claim:   mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "test-pvc"},
					SubPath: "",
				},
			},
			directoryName: testDirName,
		},
		{
			name:    "with PVC whitespace subPath uses directory name only",
			timeout: 5 * time.Second,
			storage: &mustgatherv1alpha1.Storage{
				Type: mustgatherv1alpha1.StorageTypePersistentVolume,
				PersistentVolume: mustgatherv1alpha1.PersistentVolumeConfig{
					Claim:   mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "test-pvc"},
					SubPath: "   ",
				},
			},
			directoryName: testDirName,
		},
		{
			name:    "with PVC slash-only subPath uses directory name only",
			timeout: 5 * time.Second,
			storage: &mustgatherv1alpha1.Storage{
				Type: mustgatherv1alpha1.StorageTypePersistentVolume,
				PersistentVolume: mustgatherv1alpha1.PersistentVolumeConfig{
					Claim:   mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "test-pvc"},
					SubPath: "/",
				},
			},
			directoryName: testDirName,
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			container := getGatherContainer(tt.mustGatherImage, tt.audit, tt.timeout, tt.storage, tt.caConfigMap, tt.timeFilter, tt.command, tt.args, tt.directoryName, false)

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
				wantSubPath := path.Join(base, tt.directoryName)
				if volumeMount.SubPath != wantSubPath {
					t.Fatalf("volume mount subPath was not correctly set. got %q, wanted %q", volumeMount.SubPath, wantSubPath)
				}
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

func Test_getGatherContainer_ObfuscateChown(t *testing.T) {
	container := getGatherContainer("img", false, 0, nil, "", nil, nil, nil, "dir", true)
	if len(container.Command) < 3 {
		t.Fatal("expected default bash gather command")
	}
	if !strings.Contains(container.Command[2], gatherObfuscateChownSuffix) {
		t.Fatalf("expected gather command to include chown suffix, got %q", container.Command[2])
	}

	noChown := getGatherContainer("img", false, 0, nil, "", nil, nil, nil, "dir", false)
	if strings.Contains(noChown.Command[2], gatherObfuscateChownSuffix) {
		t.Fatal("did not expect chown when obfuscation disabled")
	}
}

func Test_getUploadContainer_ObfuscateEnv(t *testing.T) {
	configRef := v1.LocalObjectReference{Name: "custom-config"}
	secretRef := v1.LocalObjectReference{Name: "secret"}

	tests := []struct {
		name       string
		obfuscate  bool
		configRef  *v1.LocalObjectReference
		wantConfig string
		wantObfusc bool
	}{
		{
			name:       "default config path",
			obfuscate:  true,
			configRef:  nil,
			wantConfig: "/etc/must-gather-clean/default-config.yaml",
			wantObfusc: true,
		},
		{
			name:       "custom config path",
			obfuscate:  true,
			configRef:  &configRef,
			wantConfig: "/etc/must-gather-clean/config.yaml",
			wantObfusc: true,
		},
		{
			name:       "obfuscate disabled",
			obfuscate:  false,
			configRef:  nil,
			wantObfusc: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			container := getUploadContainer(
				"operator-image",
				nil,
				"dir",
				nil,
				tt.configRef,
				tt.obfuscate,
				"1234",
				"host",
				false,
				&secretRef,
				"", "", "",
				false,
			)
			env := envValues(container)
			if got := env[uploadEnvObfuscate]; (got == "true") != tt.wantObfusc {
				t.Fatalf("%s=%q, want obfuscate enabled=%v", uploadEnvObfuscate, got, tt.wantObfusc)
			}
			if tt.wantObfusc {
				if env[uploadEnvObfuscateConfig] != tt.wantConfig {
					t.Fatalf("%s=%q, want %q", uploadEnvObfuscateConfig, env[uploadEnvObfuscateConfig], tt.wantConfig)
				}
			} else if _, ok := env[uploadEnvObfuscateConfig]; ok {
				t.Fatalf("did not expect %s when obfuscation disabled", uploadEnvObfuscateConfig)
			}
		})
	}
}

func Test_getJobTemplate_ObfuscateMountConsistency(t *testing.T) {
	enabled := true
	dirName := "must-gather.local.test.20240101T120000Z.000001"
	storage := &mustgatherv1alpha1.Storage{
		Type: mustgatherv1alpha1.StorageTypePersistentVolume,
		PersistentVolume: mustgatherv1alpha1.PersistentVolumeConfig{
			Claim:   mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "shared-pvc"},
			SubPath: "base-path",
		},
	}

	mg := mustgatherv1alpha1.MustGather{
		ObjectMeta: metav1.ObjectMeta{Name: "mg", Namespace: "ns"},
		Spec: mustgatherv1alpha1.MustGatherSpec{
			ServiceAccountName: "default",
			Storage:            storage,
			Obfuscate:          &mustgatherv1alpha1.ObfuscateConfig{Enabled: &enabled},
			UploadTarget: &mustgatherv1alpha1.UploadTargetSpec{
				Type: mustgatherv1alpha1.UploadTypeSFTP,
				SFTP: &mustgatherv1alpha1.SFTPSpec{
					CaseID: "1234",
					Host:   "sftp.example.com",
					CaseManagementAccountSecretRef: v1.LocalObjectReference{Name: "secret"},
				},
			},
		},
	}

	job := getJobTemplate("img", "operator-image", mg, "", dirName, "must-gather-operator")
	gather := findGatherContainerInJob(t, job)
	upload := findUploadContainerInJob(t, job)

	wantSubPath, _ := outputSubPath(storage, dirName)
	gatherSubPath := volumeMountSubPath(gather, outputVolumeName)
	uploadSubPath := volumeMountSubPath(upload, outputVolumeName)

	if gatherSubPath != wantSubPath {
		t.Fatalf("gather subPath = %q, want %q", gatherSubPath, wantSubPath)
	}
	if uploadSubPath != wantSubPath {
		t.Fatalf("upload subPath = %q, want %q", uploadSubPath, wantSubPath)
	}
	if gatherSubPath != uploadSubPath {
		t.Fatalf("gather and upload subPath mismatch: %q vs %q", gatherSubPath, uploadSubPath)
	}
}

// Test_getJobTemplate_ObfuscationPVCIsolation verifies per-invocation write isolation on a
// shared PVC (OCPBUGS-64626, plan §2): successive Jobs with the same spec.storage PVC but
// different directoryName values must use separate, non-overlapping subPaths on gather and
// upload output mounts — simulating multiple MustGather runs against one PVC.
func Test_getJobTemplate_ObfuscationPVCIsolation(t *testing.T) {
	enabled := true
	storage := &mustgatherv1alpha1.Storage{
		Type: mustgatherv1alpha1.StorageTypePersistentVolume,
		PersistentVolume: mustgatherv1alpha1.PersistentVolumeConfig{
			Claim:   mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "shared-pvc"},
			SubPath: "cluster-runs",
		},
	}
	spec := mustgatherv1alpha1.MustGatherSpec{
		ServiceAccountName: "default",
		Storage:            storage,
		Obfuscate:          &mustgatherv1alpha1.ObfuscateConfig{Enabled: &enabled},
		UploadTarget: &mustgatherv1alpha1.UploadTargetSpec{
			Type: mustgatherv1alpha1.UploadTypeSFTP,
			SFTP: &mustgatherv1alpha1.SFTPSpec{
				CaseID: "1234",
				Host:   "sftp.example.com",
				CaseManagementAccountSecretRef: v1.LocalObjectReference{Name: "secret"},
			},
		},
	}

	dirA := "must-gather.local.aaa.20240101T120000Z.000001"
	dirB := "must-gather.local.bbb.20240101T130000Z.000002"

	jobA := getJobTemplate("img", "operator-image", mustgatherv1alpha1.MustGather{
		ObjectMeta: metav1.ObjectMeta{Name: "mg-a", Namespace: "ns"},
		Spec:       spec,
	}, "", dirA, "must-gather-operator")
	jobB := getJobTemplate("img", "operator-image", mustgatherv1alpha1.MustGather{
		ObjectMeta: metav1.ObjectMeta{Name: "mg-b", Namespace: "ns"},
		Spec:       spec,
	}, "", dirB, "must-gather-operator")

	wantA, _ := outputSubPath(storage, dirA)
	wantB, _ := outputSubPath(storage, dirB)
	if wantA == wantB {
		t.Fatalf("test setup: directory subPaths must differ, both %q", wantA)
	}

	for _, tc := range []struct {
		name string
		job  *batchv1.Job
		want string
	}{
		{name: "run A gather", job: jobA, want: wantA},
		{name: "run A upload", job: jobA, want: wantA},
		{name: "run B gather", job: jobB, want: wantB},
		{name: "run B upload", job: jobB, want: wantB},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var container v1.Container
			if strings.Contains(tc.name, "gather") {
				container = findGatherContainerInJob(t, tc.job)
			} else {
				container = findUploadContainerInJob(t, tc.job)
			}
			got := volumeMountSubPath(container, outputVolumeName)
			if got != tc.want {
				t.Fatalf("subPath = %q, want %q", got, tc.want)
			}
		})
	}

	gatherA := volumeMountSubPath(findGatherContainerInJob(t, jobA), outputVolumeName)
	uploadA := volumeMountSubPath(findUploadContainerInJob(t, jobA), outputVolumeName)
	gatherB := volumeMountSubPath(findGatherContainerInJob(t, jobB), outputVolumeName)
	uploadB := volumeMountSubPath(findUploadContainerInJob(t, jobB), outputVolumeName)

	if gatherA != uploadA {
		t.Fatalf("run A gather/upload subPath mismatch: %q vs %q", gatherA, uploadA)
	}
	if gatherB != uploadB {
		t.Fatalf("run B gather/upload subPath mismatch: %q vs %q", gatherB, uploadB)
	}
	if gatherA == gatherB {
		t.Fatalf("runs must not share output subPath: both %q", gatherA)
	}
}

// Test_getJobTemplate_ObfuscationSourceSubPathSanitizedInJobMounts verifies whitespace and
// separator trimming for obfuscate.source subPath at Job template level (OCPBUGS-64626).
func Test_getJobTemplate_ObfuscationSourceSubPathSanitizedInJobMounts(t *testing.T) {
	enabled := true
	mg := mustgatherv1alpha1.MustGather{
		ObjectMeta: metav1.ObjectMeta{Name: "mg", Namespace: "ns"},
		Spec: mustgatherv1alpha1.MustGatherSpec{
			ServiceAccountName: "default",
			Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
				Enabled: &enabled,
				Source: &mustgatherv1alpha1.ObfuscateSourceConfig{
					Claim:   mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "source-pvc"},
					SubPath: "  /nested  ",
				},
			},
		},
	}

	job := getJobTemplate("img", "operator-image", mg, "", "dir-name", "must-gather-operator")
	upload := findUploadContainerInJob(t, job)

	got := volumeMountSubPath(upload, obfuscateSourceVolumeName)
	if got != "nested" {
		t.Fatalf("source mount subPath = %q, want %q", got, "nested")
	}
	if !volumeMountReadOnly(upload, obfuscateSourceVolumeName) {
		t.Fatal("expected read-only source PVC mount on upload container")
	}
}

// Test_getJobTemplate_ObfuscationStagingUsesEmptyDirUploadVolume confirms obfuscated output
// staging uses the emptyDir upload volume (FR-010), not the read-only source PVC subPath.
func Test_getJobTemplate_ObfuscationStagingUsesEmptyDirUploadVolume(t *testing.T) {
	enabled := true
	mg := mustgatherv1alpha1.MustGather{
		ObjectMeta: metav1.ObjectMeta{Name: "mg", Namespace: "ns"},
		Spec: mustgatherv1alpha1.MustGatherSpec{
			ServiceAccountName: "default",
			Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
				Enabled: &enabled,
				Source: &mustgatherv1alpha1.ObfuscateSourceConfig{
					Claim: mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "source-pvc"},
				},
			},
		},
	}

	job := getJobTemplate("img", "operator-image", mg, "", "dir-name", "must-gather-operator")

	uploadVol := findVolumeInJob(t, job, uploadVolumeName)
	if uploadVol.EmptyDir == nil {
		t.Fatalf("volume %q must be emptyDir for obfuscation staging", uploadVolumeName)
	}

	upload := findUploadContainerInJob(t, job)
	stagingMount, ok := volumeMount(upload, uploadVolumeName)
	if !ok {
		t.Fatalf("upload container missing mount for %q", uploadVolumeName)
	}
	if stagingMount.MountPath != volumeUploadMountPath {
		t.Fatalf("staging mount path = %q, want %q", stagingMount.MountPath, volumeUploadMountPath)
	}
	if stagingMount.SubPath != "" {
		t.Fatalf("staging mount must not use subPath on upload volume, got %q", stagingMount.SubPath)
	}

	for _, mount := range upload.VolumeMounts {
		if mount.MountPath != volumeUploadMountPath {
			continue
		}
		if mount.Name != uploadVolumeName {
			t.Fatalf("staging path %q must use %q, not %q", volumeUploadMountPath, uploadVolumeName, mount.Name)
		}
	}
}

func volumeMountSubPath(container v1.Container, volumeName string) string {
	for _, mount := range container.VolumeMounts {
		if mount.Name == volumeName {
			return mount.SubPath
		}
	}
	return ""
}

func Test_getUploadContainer(t *testing.T) {
	testDirName := "must-gather.local.456789abcdef.20260617T143025Z.042315"

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
		directoryName    string
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
			name:          "With PVC subPath and directory name",
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
			directoryName: testDirName,
		},
		{
			name:          "With PVC empty subPath uses directory name only",
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
			directoryName: testDirName,
		},
		{
			name:          "With PVC whitespace subPath uses directory name only",
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
			directoryName: testDirName,
		},
		{
			name:          "With PVC slash-only subPath uses directory name only",
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
			directoryName: testDirName,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFailed := false
			secretRef := tt.secretKeyRefName
			container := getUploadContainer(
				tt.operatorImage,
				tt.storage,
				tt.directoryName,
				nil,
				nil,
				false,
				tt.caseId,
				tt.host,
				tt.internalUser,
				&secretRef,
				tt.httpProxy,
				tt.httpsProxy,
				tt.noProxy,
				tt.mountCAConfigMap,
			)

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
				wantSubPath := path.Join(base, tt.directoryName)
				if outputMount.SubPath != wantSubPath {
					t.Fatalf("expected output volume mount subPath %q but got %q", wantSubPath, outputMount.SubPath)
				}
			}

			for _, env := range container.Env {
				switch env.Name {
				case uploadEnvCaseId:
					if env.Value != tt.caseId {
						t.Fatalf("expected case ID envar %v but got %v", tt.caseId, env.Value)
					}
				case uploadEnvHost:
					if env.Value != tt.host {
						t.Fatalf("expected host envar %v but got %v", tt.host, env.Value)
					}
				case uploadEnvInternalUser:
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
					if !reflect.DeepEqual(env.ValueFrom.SecretKeyRef.LocalObjectReference, tt.secretKeyRefName) {
						t.Fatalf("expected %v envar to have secret key ref name %v but got %v", env.Name, tt.secretKeyRefName.Name, env.ValueFrom.SecretKeyRef.Name)
					}
				}

				if testFailed {
					t.Error()
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

			job := getJobTemplate("img", "operator-image", mg, "", "must-gather.local.test.20240101T120000Z.000001", "must-gather-operator")
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

			job := getJobTemplate("image", "operator-image", mg, "", "must-gather.local.test.20240101T120000Z.000001", "must-gather-operator")

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

func Test_getJobTemplate_ObfuscationJobShape(t *testing.T) {
	t.Setenv(DefaultMustGatherImageEnv, "quay.io/foo/bar/must-gather:latest")

	enabled := true
	sftpUpload := &mustgatherv1alpha1.UploadTargetSpec{
		Type: mustgatherv1alpha1.UploadTypeSFTP,
		SFTP: &mustgatherv1alpha1.SFTPSpec{
			CaseID: "1234",
			Host:   "sftp.example.com",
			CaseManagementAccountSecretRef: v1.LocalObjectReference{Name: "case-mgmt-secret"},
		},
	}

	tests := []struct {
		name           string
		spec           mustgatherv1alpha1.MustGatherSpec
		wantGather     bool
		wantUpload     bool
		wantSourceVol  bool
		wantConfigVol  bool
	}{
		{
			name: "gather and obfuscate with upload",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
				Obfuscate:          &mustgatherv1alpha1.ObfuscateConfig{Enabled: &enabled},
				UploadTarget:       sftpUpload,
			},
			wantGather: true,
			wantUpload: true,
		},
		{
			name: "source obfuscate only",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
				Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
					Enabled: &enabled,
					Source: &mustgatherv1alpha1.ObfuscateSourceConfig{
						Claim: mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "source-pvc"},
					},
				},
			},
			wantGather:    false,
			wantUpload:    true,
			wantSourceVol: true,
		},
		{
			name: "source obfuscate with upload",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
				Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
					Enabled: &enabled,
					Source: &mustgatherv1alpha1.ObfuscateSourceConfig{
						Claim: mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "source-pvc"},
					},
				},
				UploadTarget: sftpUpload,
			},
			wantGather:    false,
			wantUpload:    true,
			wantSourceVol: true,
		},
		{
			name: "custom obfuscation config",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
				Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
					Enabled:              &enabled,
					ObfuscationConfigRef: &v1.LocalObjectReference{Name: "custom-obfuscate-config"},
				},
				UploadTarget: sftpUpload,
			},
			wantGather:    true,
			wantUpload:    true,
			wantConfigVol: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mg := mustgatherv1alpha1.MustGather{
				ObjectMeta: metav1.ObjectMeta{Name: "mg", Namespace: "ns"},
				Spec:       tt.spec,
			}
			job := getJobTemplate("img", "operator-image", mg, "", "dir-name", "must-gather-operator")

			hasGather := containerPresent(job, gatherContainerName)
			hasUpload := containerPresent(job, uploadContainerName)
			if hasGather != tt.wantGather {
				t.Fatalf("gather container present = %v, want %v", hasGather, tt.wantGather)
			}
			if hasUpload != tt.wantUpload {
				t.Fatalf("upload container present = %v, want %v", hasUpload, tt.wantUpload)
			}
			if got := volumePresent(job, obfuscateSourceVolumeName); got != tt.wantSourceVol {
				t.Fatalf("source volume present = %v, want %v", got, tt.wantSourceVol)
			}
			if got := volumePresent(job, obfuscateConfigVolumeName); got != tt.wantConfigVol {
				t.Fatalf("config volume present = %v, want %v", got, tt.wantConfigVol)
			}
		})
	}
}

func Test_getJobTemplate_ObfuscationCoreBranches(t *testing.T) {
	t.Setenv(DefaultMustGatherImageEnv, "quay.io/foo/bar/must-gather:latest")

	enabled := true
	sftpUpload := &mustgatherv1alpha1.UploadTargetSpec{
		Type: mustgatherv1alpha1.UploadTypeSFTP,
		SFTP: &mustgatherv1alpha1.SFTPSpec{
			CaseID: "1234",
			Host:   "sftp.example.com",
			CaseManagementAccountSecretRef: v1.LocalObjectReference{Name: "case-mgmt-secret"},
		},
	}

	tests := []struct {
		name              string
		spec              mustgatherv1alpha1.MustGatherSpec
		wantGather        bool
		wantUpload        bool
		wantObfuscateEnv  bool
		wantObfuscateCfg  string
		wantGatherChown   bool
		wantConfigVol     bool
		wantSourceVol     bool
		wantSourceReadOnly bool
		wantSFTPEnv       bool
	}{
		{
			name: "obfuscate nil backward compatible",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
				UploadTarget:       sftpUpload,
			},
			wantGather:       true,
			wantUpload:       true,
			wantObfuscateEnv: false,
			wantGatherChown:  false,
			wantSFTPEnv:      true,
		},
		{
			name: "enabled with upload",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
				Obfuscate:          &mustgatherv1alpha1.ObfuscateConfig{Enabled: &enabled},
				UploadTarget:       sftpUpload,
			},
			wantGather:       true,
			wantUpload:       true,
			wantObfuscateEnv: true,
			wantObfuscateCfg: "/etc/must-gather-clean/default-config.yaml",
			wantGatherChown:  true,
			wantSFTPEnv:      true,
		},
		{
			name: "enabled with custom obfuscationConfigRef",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
				Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
					Enabled:              &enabled,
					ObfuscationConfigRef: &v1.LocalObjectReference{Name: "custom-obfuscate-config"},
				},
				UploadTarget: sftpUpload,
			},
			wantGather:       true,
			wantUpload:       true,
			wantObfuscateEnv: true,
			wantObfuscateCfg: "/etc/must-gather-clean/config.yaml",
			wantGatherChown:  true,
			wantConfigVol:    true,
			wantSFTPEnv:      true,
		},
		{
			name: "enabled with source PVC",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
				Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
					Enabled: &enabled,
					Source: &mustgatherv1alpha1.ObfuscateSourceConfig{
						Claim: mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "source-pvc"},
					},
				},
			},
			wantGather:         false,
			wantUpload:         true,
			wantObfuscateEnv:   true,
			wantObfuscateCfg:   "/etc/must-gather-clean/default-config.yaml",
			wantSourceVol:      true,
			wantSourceReadOnly: true,
		},
		{
			name: "enabled with source PVC and upload",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
				Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
					Enabled: &enabled,
					Source: &mustgatherv1alpha1.ObfuscateSourceConfig{
						Claim: mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "source-pvc"},
					},
				},
				UploadTarget: sftpUpload,
			},
			wantGather:         false,
			wantUpload:         true,
			wantObfuscateEnv:   true,
			wantObfuscateCfg:   "/etc/must-gather-clean/default-config.yaml",
			wantSourceVol:      true,
			wantSourceReadOnly: true,
			wantSFTPEnv:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mg := mustgatherv1alpha1.MustGather{
				ObjectMeta: metav1.ObjectMeta{Name: "mg", Namespace: "ns"},
				Spec:       tt.spec,
			}
			job := getJobTemplate("img", "operator-image", mg, "", "dir-name", "must-gather-operator")

			if got := containerPresent(job, gatherContainerName); got != tt.wantGather {
				t.Fatalf("gather present = %v, want %v", got, tt.wantGather)
			}
			if got := containerPresent(job, uploadContainerName); got != tt.wantUpload {
				t.Fatalf("upload present = %v, want %v", got, tt.wantUpload)
			}
			if got := volumePresent(job, obfuscateConfigVolumeName); got != tt.wantConfigVol {
				t.Fatalf("config volume present = %v, want %v", got, tt.wantConfigVol)
			}
			if got := volumePresent(job, obfuscateSourceVolumeName); got != tt.wantSourceVol {
				t.Fatalf("source volume present = %v, want %v", got, tt.wantSourceVol)
			}

			if tt.wantGather {
				gather := findGatherContainerInJob(t, job)
				hasChown := len(gather.Command) >= 3 && strings.Contains(gather.Command[2], gatherObfuscateChownSuffix)
				if hasChown != tt.wantGatherChown {
					t.Fatalf("gather chown = %v, want %v", hasChown, tt.wantGatherChown)
				}
				if gather.Name != gatherContainerName {
					t.Fatalf("gather container name = %q", gather.Name)
				}
			}

			if tt.wantUpload {
				upload := findUploadContainerInJob(t, job)
				if upload.Name != uploadContainerName {
					t.Fatalf("upload container name = %q", upload.Name)
				}

				env := envValues(upload)
				if got := env[uploadEnvObfuscate] == "true"; got != tt.wantObfuscateEnv {
					t.Fatalf("%s enabled = %v, want %v", uploadEnvObfuscate, got, tt.wantObfuscateEnv)
				}
				if tt.wantObfuscateEnv {
					if env[uploadEnvObfuscateConfig] != tt.wantObfuscateCfg {
						t.Fatalf("%s = %q, want %q", uploadEnvObfuscateConfig, env[uploadEnvObfuscateConfig], tt.wantObfuscateCfg)
					}
				} else if _, ok := env[uploadEnvObfuscateConfig]; ok {
					t.Fatalf("did not expect %s when obfuscation disabled", uploadEnvObfuscateConfig)
				}

				if tt.wantSourceReadOnly {
					if !volumeMountReadOnly(upload, obfuscateSourceVolumeName) {
						t.Fatal("expected read-only source PVC mount on upload container")
					}
				}

				if tt.wantSFTPEnv {
					if env[uploadEnvCaseId] != "1234" {
						t.Fatalf("%s = %q, want 1234", uploadEnvCaseId, env[uploadEnvCaseId])
					}
					if !envVarHasSecretRef(upload, uploadEnvUsername, "case-mgmt-secret") {
						t.Fatalf("expected %s from secret case-mgmt-secret", uploadEnvUsername)
					}
				}
			}
		})
	}
}

// Test_getJobTemplate_ObfuscationBackwardCompatAndDisabled verifies SC-007: absent or
// explicitly disabled obfuscate must not inject obfuscation volumes, env vars, or gather chown.
func Test_getJobTemplate_ObfuscationBackwardCompatAndDisabled(t *testing.T) {
	t.Setenv(DefaultMustGatherImageEnv, "quay.io/foo/bar/must-gather:latest")

	disabled := false
	sftpUpload := &mustgatherv1alpha1.UploadTargetSpec{
		Type: mustgatherv1alpha1.UploadTypeSFTP,
		SFTP: &mustgatherv1alpha1.SFTPSpec{
			CaseID: "1234",
			Host:   "sftp.example.com",
			CaseManagementAccountSecretRef: v1.LocalObjectReference{Name: "case-mgmt-secret"},
		},
	}

	tests := []struct {
		name       string
		spec       mustgatherv1alpha1.MustGatherSpec
		wantUpload bool
	}{
		{
			name: "obfuscate field absent with upload",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
				UploadTarget:       sftpUpload,
			},
			wantUpload: true,
		},
		{
			name: "obfuscate enabled false with upload",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
				Obfuscate:          &mustgatherv1alpha1.ObfuscateConfig{Enabled: &disabled},
				UploadTarget:       sftpUpload,
			},
			wantUpload: true,
		},
		{
			name: "obfuscate absent gather only",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
			},
			wantUpload: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mg := mustgatherv1alpha1.MustGather{
				ObjectMeta: metav1.ObjectMeta{Name: "mg", Namespace: "ns"},
				Spec:       tt.spec,
			}
			job := getJobTemplate("img", "operator-image", mg, "", "dir-name", "must-gather-operator")

			if !containerPresent(job, gatherContainerName) {
				t.Fatal("expected gather container for backward-compatible spec")
			}
			if got := containerPresent(job, uploadContainerName); got != tt.wantUpload {
				t.Fatalf("upload present = %v, want %v", got, tt.wantUpload)
			}
			if volumePresent(job, obfuscateConfigVolumeName) {
				t.Fatal("did not expect obfuscation config volume when obfuscate disabled or absent")
			}
			if volumePresent(job, obfuscateSourceVolumeName) {
				t.Fatal("did not expect obfuscation source volume when obfuscate disabled or absent")
			}

			gather := findGatherContainerInJob(t, job)
			if strings.Contains(gather.Command[2], gatherObfuscateChownSuffix) {
				t.Fatal("did not expect gather chown when obfuscate disabled or absent")
			}

			if tt.wantUpload {
				upload := findUploadContainerInJob(t, job)
				env := envValues(upload)
				if env[uploadEnvObfuscate] == "true" {
					t.Fatalf("did not expect %s=true when obfuscate disabled or absent", uploadEnvObfuscate)
				}
				if _, ok := env[uploadEnvObfuscateConfig]; ok {
					t.Fatalf("did not expect %s when obfuscate disabled or absent", uploadEnvObfuscateConfig)
				}
			}
		})
	}
}

// Test_getJobTemplate_ObfuscationSourceSubPathEdgeCasesInMounts verifies empty, whitespace-only,
// and separator-only obfuscate.source subPath values omit SubPath on the source PVC mount.
func Test_getJobTemplate_ObfuscationSourceSubPathEdgeCasesInMounts(t *testing.T) {
	enabled := true
	subPaths := []struct {
		name    string
		subPath string
	}{
		{name: "empty subPath", subPath: ""},
		{name: "whitespace-only subPath", subPath: "   "},
		{name: "slash-only subPath", subPath: "/"},
		{name: "double-slash subPath", subPath: "//"},
	}

	for _, tt := range subPaths {
		t.Run(tt.name, func(t *testing.T) {
			mg := mustgatherv1alpha1.MustGather{
				ObjectMeta: metav1.ObjectMeta{Name: "mg", Namespace: "ns"},
				Spec: mustgatherv1alpha1.MustGatherSpec{
					ServiceAccountName: "default",
					Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
						Enabled: &enabled,
						Source: &mustgatherv1alpha1.ObfuscateSourceConfig{
							Claim:   mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "source-pvc"},
							SubPath: tt.subPath,
						},
					},
				},
			}

			job := getJobTemplate("img", "operator-image", mg, "", "dir-name", "must-gather-operator")
			upload := findUploadContainerInJob(t, job)

			got := volumeMountSubPath(upload, obfuscateSourceVolumeName)
			if got != "" {
				t.Fatalf("source mount subPath = %q, want empty for sanitized invalid subPath", got)
			}
			if !volumeMountReadOnly(upload, obfuscateSourceVolumeName) {
				t.Fatal("expected read-only source PVC mount on upload container")
			}
		})
	}
}

// Test_getJobTemplate_ObfuscationThreeModeMatrix documents and verifies the three obfuscation
// Job shapes (Mode 1 gather+obfuscate+upload, Mode 2 source-only, Mode 3 source+upload).
func Test_getJobTemplate_ObfuscationThreeModeMatrix(t *testing.T) {
	t.Setenv(DefaultMustGatherImageEnv, "quay.io/foo/bar/must-gather:latest")

	enabled := true
	sftpUpload := &mustgatherv1alpha1.UploadTargetSpec{
		Type: mustgatherv1alpha1.UploadTypeSFTP,
		SFTP: &mustgatherv1alpha1.SFTPSpec{
			CaseID: "1234",
			Host:   "sftp.example.com",
			CaseManagementAccountSecretRef: v1.LocalObjectReference{Name: "case-mgmt-secret"},
		},
	}
	source := &mustgatherv1alpha1.ObfuscateSourceConfig{
		Claim: mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "source-pvc"},
	}

	tests := []struct {
		mode           string
		spec           mustgatherv1alpha1.MustGatherSpec
		wantGather     bool
		wantUpload     bool
		wantSourceVol  bool
		wantStagingDir bool
		wantChown      bool
	}{
		{
			mode: "mode 1 gather obfuscate upload",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
				Obfuscate:          &mustgatherv1alpha1.ObfuscateConfig{Enabled: &enabled},
				UploadTarget:       sftpUpload,
			},
			wantGather:     true,
			wantUpload:     true,
			wantStagingDir: true,
			wantChown:      true,
		},
		{
			mode: "mode 2 source obfuscate only",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
				Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
					Enabled: &enabled,
					Source:  source,
				},
			},
			wantGather:     false,
			wantUpload:     true,
			wantSourceVol:  true,
			wantStagingDir: true,
		},
		{
			mode: "mode 3 source obfuscate with upload",
			spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountName: "default",
				Obfuscate: &mustgatherv1alpha1.ObfuscateConfig{
					Enabled: &enabled,
					Source:  source,
				},
				UploadTarget: sftpUpload,
			},
			wantGather:     false,
			wantUpload:     true,
			wantSourceVol:  true,
			wantStagingDir: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			mg := mustgatherv1alpha1.MustGather{
				ObjectMeta: metav1.ObjectMeta{Name: "mg", Namespace: "ns"},
				Spec:       tt.spec,
			}
			job := getJobTemplate("img", "operator-image", mg, "", "dir-name", "must-gather-operator")

			if got := containerPresent(job, gatherContainerName); got != tt.wantGather {
				t.Fatalf("gather = %v, want %v", got, tt.wantGather)
			}
			if got := containerPresent(job, uploadContainerName); got != tt.wantUpload {
				t.Fatalf("upload = %v, want %v", got, tt.wantUpload)
			}
			if got := volumePresent(job, obfuscateSourceVolumeName); got != tt.wantSourceVol {
				t.Fatalf("source volume = %v, want %v", got, tt.wantSourceVol)
			}

			if tt.wantGather {
				gather := findGatherContainerInJob(t, job)
				hasChown := strings.Contains(gather.Command[2], gatherObfuscateChownSuffix)
				if hasChown != tt.wantChown {
					t.Fatalf("gather chown = %v, want %v", hasChown, tt.wantChown)
				}
			}

			if tt.wantStagingDir {
				uploadVol := findVolumeInJob(t, job, uploadVolumeName)
				if uploadVol.EmptyDir == nil {
					t.Fatalf("volume %q must be emptyDir for obfuscation staging", uploadVolumeName)
				}
				upload := findUploadContainerInJob(t, job)
				if env := envValues(upload); env[uploadEnvObfuscate] != "true" {
					t.Fatalf("expected %s=true for %s", uploadEnvObfuscate, tt.mode)
				}
			}
		})
	}
}

func volumeMountReadOnly(container v1.Container, volumeName string) bool {
	for _, mount := range container.VolumeMounts {
		if mount.Name == volumeName {
			return mount.ReadOnly
		}
	}
	return false
}

func envVarHasSecretRef(container v1.Container, envName, secretName string) bool {
	for _, e := range container.Env {
		if e.Name != envName || e.ValueFrom == nil || e.ValueFrom.SecretKeyRef == nil {
			continue
		}
		return e.ValueFrom.SecretKeyRef.Name == secretName
	}
	return false
}

func containerPresent(job *batchv1.Job, name string) bool {
	for _, c := range job.Spec.Template.Spec.Containers {
		if c.Name == name {
			return true
		}
	}
	return false
}

func volumePresent(job *batchv1.Job, name string) bool {
	for _, v := range job.Spec.Template.Spec.Volumes {
		if v.Name == name {
			return true
		}
	}
	return false
}

func findVolumeInJob(t *testing.T, job *batchv1.Job, name string) v1.Volume {
	t.Helper()
	for _, v := range job.Spec.Template.Spec.Volumes {
		if v.Name == name {
			return v
		}
	}
	t.Fatalf("volume %q not found in job", name)
	return v1.Volume{}
}

func volumeMount(container v1.Container, volumeName string) (v1.VolumeMount, bool) {
	for _, mount := range container.VolumeMounts {
		if mount.Name == volumeName {
			return mount, true
		}
	}
	return v1.VolumeMount{}, false
}

func Test_getJobTemplate_FilenamePrefix(t *testing.T) {
	t.Setenv(DefaultMustGatherImageEnv, "quay.io/foo/bar/must-gather:latest")

	directoryName := "must-gather.local.456789abcdef.20260617T143025Z.042315"

	mg := mustgatherv1alpha1.MustGather{
		ObjectMeta: metav1.ObjectMeta{Name: "mg", Namespace: "ns"},
		Spec: mustgatherv1alpha1.MustGatherSpec{
			ServiceAccountName: "default",
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

	job := getJobTemplate("img", "operator-image", mg, "", directoryName, "must-gather-operator")
	upload := findUploadContainerInJob(t, job)
	uploadEnv := envValues(upload)

	val, ok := uploadEnv[uploadEnvFilenamePrefix]
	if !ok {
		t.Fatalf("expected %s env var in upload container, not found", uploadEnvFilenamePrefix)
	}
	if val != directoryName {
		t.Fatalf("expected %s=%s, got %s", uploadEnvFilenamePrefix, directoryName, val)
	}
}

func Test_outputSubPath(t *testing.T) {
	tests := []struct {
		name          string
		storage       *mustgatherv1alpha1.Storage
		directoryName string
		wantPath      string
		wantOk        bool
	}{
		{
			name:     "nil storage returns empty",
			storage:  nil,
			wantPath: "",
			wantOk:   false,
		},
		{
			name: "PVC with subPath and directoryName",
			storage: &mustgatherv1alpha1.Storage{
				Type: mustgatherv1alpha1.StorageTypePersistentVolume,
				PersistentVolume: mustgatherv1alpha1.PersistentVolumeConfig{
					Claim:   mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "pvc"},
					SubPath: "base-path",
				},
			},
			directoryName: "must-gather.local.abc.20260101T000000Z.123456",
			wantPath:      "base-path/must-gather.local.abc.20260101T000000Z.123456",
			wantOk:        true,
		},
		{
			name: "PVC with empty subPath and directoryName",
			storage: &mustgatherv1alpha1.Storage{
				Type: mustgatherv1alpha1.StorageTypePersistentVolume,
				PersistentVolume: mustgatherv1alpha1.PersistentVolumeConfig{
					Claim:   mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "pvc"},
					SubPath: "",
				},
			},
			directoryName: "must-gather.local.abc.20260101T000000Z.123456",
			wantPath:      "must-gather.local.abc.20260101T000000Z.123456",
			wantOk:        true,
		},
		{
			name: "PVC with whitespace subPath and directoryName",
			storage: &mustgatherv1alpha1.Storage{
				Type: mustgatherv1alpha1.StorageTypePersistentVolume,
				PersistentVolume: mustgatherv1alpha1.PersistentVolumeConfig{
					Claim:   mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "pvc"},
					SubPath: "  / ",
				},
			},
			directoryName: "must-gather.local.abc.20260101T000000Z.123456",
			wantPath:      "must-gather.local.abc.20260101T000000Z.123456",
			wantOk:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPath, gotOk := outputSubPath(tt.storage, tt.directoryName)
			if gotOk != tt.wantOk {
				t.Fatalf("outputSubPath() ok = %v, want %v", gotOk, tt.wantOk)
			}
			if gotPath != tt.wantPath {
				t.Fatalf("outputSubPath() path = %q, want %q", gotPath, tt.wantPath)
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

func Test_obfuscationEnvConstants(t *testing.T) {
	if uploadEnvObfuscate != "obfuscate" {
		t.Fatalf("uploadEnvObfuscate = %q, want obfuscate", uploadEnvObfuscate)
	}
	if uploadEnvObfuscateConfig != "obfuscate_config" {
		t.Fatalf("uploadEnvObfuscateConfig = %q, want obfuscate_config", uploadEnvObfuscateConfig)
	}
}

func Test_obfuscateConfigVolumeConstants(t *testing.T) {
	if obfuscateConfigVolumeName != "obfuscate-config" {
		t.Fatalf("obfuscateConfigVolumeName = %q, want obfuscate-config", obfuscateConfigVolumeName)
	}
	if obfuscateConfigMountDir != "/etc/must-gather-clean" {
		t.Fatalf("obfuscateConfigMountDir = %q, want /etc/must-gather-clean", obfuscateConfigMountDir)
	}
	if obfuscateConfigMapKey != "config.yaml" {
		t.Fatalf("obfuscateConfigMapKey = %q, want config.yaml", obfuscateConfigMapKey)
	}
	if got := customObfuscateConfigPath(); got != "/etc/must-gather-clean/config.yaml" {
		t.Fatalf("customObfuscateConfigPath() = %q, want /etc/must-gather-clean/config.yaml", got)
	}
}

func Test_obfuscateEnabled(t *testing.T) {
	enabled := true
	disabled := false

	tests := []struct {
		name string
		spec *mustgatherv1alpha1.ObfuscateConfig
		want bool
	}{
		{name: "nil spec", spec: nil, want: false},
		{name: "nil enabled pointer", spec: &mustgatherv1alpha1.ObfuscateConfig{}, want: false},
		{name: "enabled false", spec: &mustgatherv1alpha1.ObfuscateConfig{Enabled: &disabled}, want: false},
		{name: "enabled true", spec: &mustgatherv1alpha1.ObfuscateConfig{Enabled: &enabled}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := obfuscateEnabled(tt.spec); got != tt.want {
				t.Fatalf("obfuscateEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_sourceSubPath(t *testing.T) {
	tests := []struct {
		name      string
		source    *mustgatherv1alpha1.ObfuscateSourceConfig
		wantPath  string
		wantHasSP bool
	}{
		{name: "nil source", source: nil, wantPath: "", wantHasSP: false},
		{name: "empty subPath", source: &mustgatherv1alpha1.ObfuscateSourceConfig{Claim: mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "pvc"}}, wantPath: "", wantHasSP: false},
		{name: "whitespace-only subPath", source: &mustgatherv1alpha1.ObfuscateSourceConfig{Claim: mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "pvc"}, SubPath: "   "}, wantPath: "", wantHasSP: false},
		{name: "separator-only subPath slash", source: &mustgatherv1alpha1.ObfuscateSourceConfig{Claim: mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "pvc"}, SubPath: "/"}, wantPath: "", wantHasSP: false},
		{name: "separator-only subPath double slash", source: &mustgatherv1alpha1.ObfuscateSourceConfig{Claim: mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "pvc"}, SubPath: "//"}, wantPath: "", wantHasSP: false},
		{name: "valid nested path", source: &mustgatherv1alpha1.ObfuscateSourceConfig{Claim: mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "pvc"}, SubPath: "valid/nested/path"}, wantPath: "valid/nested/path", wantHasSP: true},
		{name: "trimmed path", source: &mustgatherv1alpha1.ObfuscateSourceConfig{Claim: mustgatherv1alpha1.PersistentVolumeClaimReference{Name: "pvc"}, SubPath: " /trimmed/ "}, wantPath: "trimmed", wantHasSP: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPath, gotHasSP := sourceSubPath(tt.source)
			if gotPath != tt.wantPath {
				t.Fatalf("sourceSubPath() path = %q, want %q", gotPath, tt.wantPath)
			}
			if gotHasSP != tt.wantHasSP {
				t.Fatalf("sourceSubPath() hasSubPath = %v, want %v", gotHasSP, tt.wantHasSP)
			}
		})
	}
}
