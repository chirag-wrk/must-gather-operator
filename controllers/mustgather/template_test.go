package mustgather

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	mustgatherv1alpha1 "github.com/openshift/must-gather-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/api/core/v1"
	batchv1 "k8s.io/api/batch/v1"
)

func Test_initializeJobTemplate(t *testing.T) {
	testFailed := false
	testName := "testName"
	testNamespace := "testNamespace"
	testServiceAccountRef := "testServiceAccountRef"
	job := initializeJobTemplate(testName, testNamespace, testServiceAccountRef)

	if got := job.Name; got != testName {
		t.Logf("job name from initializeJobTemplate() was not correctly set. got %v, wanted %v", got, testName)
		testFailed = true
	}

	if got := job.Namespace; got != testNamespace {
		t.Logf("job namespace from initializeJobTemplate() was not correctly set. got %v, wanted %v", got, testNamespace)
		testFailed = true
	}

	if got := job.Spec.Template.Spec.ServiceAccountName; got != testServiceAccountRef {
		t.Logf("job service account name from initializeJobTemplate() was not correctly set. got %v, wanted %v", got, testServiceAccountRef)
		testFailed = true
	}

	if testFailed == true {
		t.Error()
	}
}

func assertJobUsesEmptyDirOnly(t *testing.T, job *batchv1.Job) {
	t.Helper()

	if len(job.Spec.Template.Spec.Volumes) != 2 {
		t.Fatalf("expected 2 volumes, got %d", len(job.Spec.Template.Spec.Volumes))
	}

	wantNames := map[string]bool{outputVolumeName: false, uploadVolumeName: false}
	for _, vol := range job.Spec.Template.Spec.Volumes {
		if vol.PersistentVolumeClaim != nil || vol.HostPath != nil || vol.ConfigMap != nil {
			t.Fatalf("volume %q must use emptyDir only, got unsupported volume source", vol.Name)
		}
		if vol.EmptyDir == nil {
			t.Fatalf("volume %q expected emptyDir, got %#v", vol.Name, vol.VolumeSource)
		}
		wantNames[vol.Name] = true
	}
	for name, seen := range wantNames {
		if !seen {
			t.Fatalf("missing expected volume %q", name)
		}
	}
}

func Test_initializeJobTemplate_EmptyDirVolumes(t *testing.T) {
	job := initializeJobTemplate("mg", "ns", "sa")
	assertJobUsesEmptyDirOnly(t, job)
}

// Test_getJobTemplate_PerJobEmptyDirIsolation documents MG-53 storage posture:
// Each MustGather run creates a Job with independent emptyDir volumes. Successive runs
// (separate Jobs/Pods) therefore use separate ephemeral storage with no cross-run data
// sharing at the volume layer. PVC-backed persistence and multi-run directory isolation
// via subPathExpr are out of scope for MG-53 and deferred to a future change.
func Test_getJobTemplate_PerJobEmptyDirIsolation(t *testing.T) {
	makeMG := func(name string, uploadEnabled bool) mustgatherv1alpha1.MustGather {
		spec := mustgatherv1alpha1.MustGatherSpec{
			ServiceAccountRef: v1.LocalObjectReference{Name: "sa"},
		}
		if uploadEnabled {
			spec.UploadTarget = sftpUploadTarget("12345678", "sec", true)
		}
		return mustgatherv1alpha1.MustGather{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"},
			Spec:       spec,
		}
	}

	t.Run("successive Jobs each use independent emptyDir volumes", func(t *testing.T) {
		jobFirstRun := getJobTemplate("operator-image", makeMG("mg-run-1", true))
		jobSecondRun := getJobTemplate("operator-image", makeMG("mg-run-2", true))

		assertJobUsesEmptyDirOnly(t, jobFirstRun)
		assertJobUsesEmptyDirOnly(t, jobSecondRun)

		if &jobFirstRun.Spec.Template.Spec.Volumes[0] == &jobSecondRun.Spec.Template.Spec.Volumes[0] {
			t.Fatal("successive Job templates must not share volume slice memory")
		}
	})

	t.Run("gather-only Job uses emptyDir without PVC", func(t *testing.T) {
		assertJobUsesEmptyDirOnly(t, getJobTemplate("operator-image", makeMG("mg-gather-only", false)))
	})
}

func Test_getGatherContainer(t *testing.T) {
	tests := []struct {
		name             string
		audit            bool
		timeout          time.Duration
		mustGatherImage  string
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFailed := false

			t.Setenv(defaultMustGatherImageEnv, tt.mustGatherImage)
			expectedImage := tt.mustGatherImage

			container := getGatherContainer(tt.audit, tt.timeout)

			containerCommand := container.Command[2]
			if tt.audit && !strings.Contains(containerCommand, gatherCommandBinaryAudit) {
				t.Logf("gather container command expected with binary %v but it wasn't present", gatherCommandBinaryAudit)
				testFailed = true
			} else if !tt.audit && !strings.Contains(containerCommand, gatherCommandBinaryNoAudit) {
				t.Logf("gather container command expected with binary %v but it wasn't present", gatherCommandBinaryNoAudit)
				testFailed = true
			}

			if !strings.HasPrefix(containerCommand, fmt.Sprintf("timeout %v", tt.timeout)) {
				t.Logf("the duration was not properly added to the container command, got %v but wanted %v", strings.Split(containerCommand, " ")[1], tt.timeout.String())
				testFailed = true
			}

			if container.Image != expectedImage {
				t.Logf("expected container image %v but got %v", expectedImage, container.Image)
				testFailed = true
			}

			if testFailed {
				t.Error()
			}
		})
	}
}

func Test_getUploadContainer(t *testing.T) {
	tests := []struct {
		name             string
		operatorImage    string
		caseId           string
		internalUser     bool
		httpProxy        string
		httpsProxy       string
		noProxy          string
		secretKeyRefName v1.LocalObjectReference
	}{
		{
			name:             "All fields present",
			operatorImage:    "testImage",
			caseId:           "1234",
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFailed := false
			container := getUploadContainer(tt.operatorImage, tt.caseId, tt.internalUser, defaultSFTPHost, tt.httpProxy, tt.httpsProxy, tt.noProxy, tt.secretKeyRefName)

			if container.Image != tt.operatorImage {
				t.Logf("expected container image %v but got %v", tt.operatorImage, container.Image)
				testFailed = true
			}

			for _, env := range container.Env {
				switch env.Name {
				case uploadEnvCaseId:
					if env.Value != tt.caseId {
						t.Logf("expected case ID envar %v but got %v", tt.caseId, env.Value)
						testFailed = true
					}
				case uploadEnvInternalUser:
					if env.Value != strconv.FormatBool(tt.internalUser) {
						t.Logf("expected internal user envar %v but got %v", tt.internalUser, env.Value)
						testFailed = true
					}
				case uploadEnvHttpProxy:
					if env.Value != tt.httpProxy {
						t.Logf("expected httpproxy envar %v but got %v", tt.httpProxy, tt.httpProxy)
						testFailed = true
					}
				case uploadEnvHttpsProxy:
					if env.Value != tt.httpsProxy {
						t.Logf("expected httpsproxy envar %v but got %v", tt.httpsProxy, tt.httpsProxy)
						testFailed = true
					}
				case uploadEnvNoProxy:
					if env.Value != tt.noProxy {
						t.Logf("expected noproxy envar %v but got %v", tt.noProxy, env.Value)
						testFailed = true
					}
				case uploadEnvSFTPHost:
					if env.Value != defaultSFTPHost {
						t.Logf("expected SFTP host envar %v but got %v", defaultSFTPHost, env.Value)
						testFailed = true
					}
				case uploadEnvUsername, uploadEnvPassword:
					if !reflect.DeepEqual(env.ValueFrom.SecretKeyRef.LocalObjectReference, tt.secretKeyRefName) {
						t.Logf("expected %v envar to have secret key ref name %v but got %v", env.Name, tt.secretKeyRefName.Name, env.ValueFrom.SecretKeyRef.Name)
						testFailed = true
					}
				}

				if testFailed {
					t.Error()
				}
			}
		})
	}
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

// helper to build SFTP uploadTarget for tests
func sftpUploadTarget(caseID, secretName string, internalUser bool) *mustgatherv1alpha1.UploadTarget {
	return &mustgatherv1alpha1.UploadTarget{
		Type: mustgatherv1alpha1.UploadTypeSFTP,
		SFTP: &mustgatherv1alpha1.SFTPUploadTargetConfig{
			CaseID:                         caseID,
			CaseManagementAccountSecretRef: v1.LocalObjectReference{Name: secretName},
			InternalUser:                   internalUser,
		},
	}
}

func Test_resolveSFTPHost(t *testing.T) {
	tests := []struct {
		name  string
		host  string
		want  string
	}{
		{
			name: "empty host falls back to default",
			host: "",
			want: defaultSFTPHost,
		},
		{
			name: "whitespace-only host falls back to default",
			host: "   ",
			want: defaultSFTPHost,
		},
		{
			name: "tab and newline trimmed to default",
			host: "\t\n",
			want: defaultSFTPHost,
		},
		{
			name: "staging hostname preserved",
			host: "sftp.access.stage.redhat.com",
			want: "sftp.access.stage.redhat.com",
		},
		{
			name: "staging hostname trimmed",
			host: "  sftp.access.stage.redhat.com  ",
			want: "sftp.access.stage.redhat.com",
		},
		{
			name: "non-empty odd string passed through after trim",
			host: "  ---  ",
			want: "---",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveSFTPHost(tt.host); got != tt.want {
				t.Fatalf("resolveSFTPHost(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}

func Test_getJobTemplate_SFTPHostEnv(t *testing.T) {
	tests := []struct {
		name string
		host string
		want string
	}{
		{
			name: "omitted host uses default in upload env",
			host: "",
			want: defaultSFTPHost,
		},
		{
			name: "whitespace-only host uses default in upload env",
			host: "  \t  ",
			want: defaultSFTPHost,
		},
		{
			name: "custom staging host wired to SFTP_HOST env",
			host: "sftp.access.stage.redhat.com",
			want: "sftp.access.stage.redhat.com",
		},
		{
			name: "custom host trimmed in upload env",
			host: "  sftp.access.stage.redhat.com  ",
			want: "sftp.access.stage.redhat.com",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := sftpUploadTarget("12345678", "sec", true)
			target.SFTP.Host = tt.host

			mg := mustgatherv1alpha1.MustGather{
				ObjectMeta: metav1.ObjectMeta{Name: "mg", Namespace: "ns"},
				Spec: mustgatherv1alpha1.MustGatherSpec{
					ServiceAccountRef: v1.LocalObjectReference{Name: "sa"},
					UploadTarget:      target,
				},
			}

			job := getJobTemplate("operator-image", mg)
			upload := findUploadContainerInJob(t, job)
			got := envValues(upload)

			if got[uploadEnvSFTPHost] != tt.want {
				t.Fatalf("%s: got SFTP_HOST=%q, want %q", uploadEnvSFTPHost, got[uploadEnvSFTPHost], tt.want)
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

// helper to find volume mount by volume name
func volumeMount(mounts []v1.VolumeMount, name string) (v1.VolumeMount, bool) {
	for _, m := range mounts {
		if m.Name == name {
			return m, true
		}
	}
	return v1.VolumeMount{}, false
}

func Test_getJobTemplate_MountConsistency(t *testing.T) {
	t.Run("gather and upload share output volume mount", func(t *testing.T) {
		mg := mustgatherv1alpha1.MustGather{
			ObjectMeta: metav1.ObjectMeta{Name: "mg", Namespace: "ns"},
			Spec: mustgatherv1alpha1.MustGatherSpec{
				ServiceAccountRef: v1.LocalObjectReference{Name: "sa"},
				UploadTarget:      sftpUploadTarget("12345678", "sec", true),
			},
		}

		job := getJobTemplate("operator-image", mg)
		gather := findGatherContainerInJob(t, job)
		upload := findUploadContainerInJob(t, job)

		gMount, ok := volumeMount(gather.VolumeMounts, outputVolumeName)
		if !ok {
			t.Fatalf("gather missing mount for %s", outputVolumeName)
		}
		uMount, ok := volumeMount(upload.VolumeMounts, outputVolumeName)
		if !ok {
			t.Fatalf("upload missing mount for %s", outputVolumeName)
		}

		if gMount.MountPath != volumeMountPath || uMount.MountPath != volumeMountPath {
			t.Fatalf("output volume mount paths differ: gather=%s upload=%s want=%s", gMount.MountPath, uMount.MountPath, volumeMountPath)
		}
		if gMount.Name != uMount.Name {
			t.Fatalf("output volume names differ: gather=%s upload=%s", gMount.Name, uMount.Name)
		}

		uploadOnly, ok := volumeMount(upload.VolumeMounts, uploadVolumeName)
		if !ok {
			t.Fatalf("upload missing mount for %s", uploadVolumeName)
		}
		if uploadOnly.MountPath != volumeUploadMountPath {
			t.Fatalf("upload staging mount path: got %s want %s", uploadOnly.MountPath, volumeUploadMountPath)
		}
	})

	t.Run("hostname change does not alter mounts", func(t *testing.T) {
		makeMG := func(host string) mustgatherv1alpha1.MustGather {
			target := sftpUploadTarget("12345678", "sec", true)
			if host != "" {
				target.SFTP.Host = host
			}
			return mustgatherv1alpha1.MustGather{
				ObjectMeta: metav1.ObjectMeta{Name: "mg", Namespace: "ns"},
				Spec: mustgatherv1alpha1.MustGatherSpec{
					ServiceAccountRef: v1.LocalObjectReference{Name: "sa"},
					UploadTarget:      target,
				},
			}
		}

		jobDefault := getJobTemplate("op", makeMG(""))
		jobStaging := getJobTemplate("op", makeMG("sftp.access.stage.redhat.com"))

		gatherDefault := findGatherContainerInJob(t, jobDefault)
		gatherStaging := findGatherContainerInJob(t, jobStaging)
		uploadDefault := findUploadContainerInJob(t, jobDefault)
		uploadStaging := findUploadContainerInJob(t, jobStaging)

		if !reflect.DeepEqual(gatherDefault.VolumeMounts, gatherStaging.VolumeMounts) {
			t.Fatalf("gather mounts differ when host changes:\ndefault=%v\nstaging=%v", gatherDefault.VolumeMounts, gatherStaging.VolumeMounts)
		}
		if !reflect.DeepEqual(uploadDefault.VolumeMounts, uploadStaging.VolumeMounts) {
			t.Fatalf("upload mounts differ when host changes:\ndefault=%v\nstaging=%v", uploadDefault.VolumeMounts, uploadStaging.VolumeMounts)
		}
	})
}

func Test_getJobTemplate_JobShapes(t *testing.T) {
	tests := []struct {
		name           string
		uploadTarget   *mustgatherv1alpha1.UploadTarget
		wantContainers int
		wantUpload     bool
	}{
		{
			name:           "gather only when uploadTarget nil",
			uploadTarget:   nil,
			wantContainers: 1,
			wantUpload:     false,
		},
		{
			name:           "gather and upload when SFTP configured",
			uploadTarget:   sftpUploadTarget("12345678", "sec", true),
			wantContainers: 2,
			wantUpload:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mg := mustgatherv1alpha1.MustGather{
				ObjectMeta: metav1.ObjectMeta{Name: "mg", Namespace: "ns"},
				Spec: mustgatherv1alpha1.MustGatherSpec{
					ServiceAccountRef: v1.LocalObjectReference{Name: "sa"},
					UploadTarget:      tt.uploadTarget,
				},
			}

			job := getJobTemplate("operator-image", mg)
			if got := len(job.Spec.Template.Spec.Containers); got != tt.wantContainers {
				t.Fatalf("container count: got %d, want %d", got, tt.wantContainers)
			}

			hasGather := false
			hasUpload := false
			for _, c := range job.Spec.Template.Spec.Containers {
				switch c.Name {
				case gatherContainerName:
					hasGather = true
				case uploadContainerName:
					hasUpload = true
				}
			}
			if !hasGather {
				t.Fatal("expected gather container")
			}
			if hasUpload != tt.wantUpload {
				t.Fatalf("upload container present: got %v, want %v", hasUpload, tt.wantUpload)
			}
		})
	}
}

func Test_getJobTemplate_FallbackWhenOnlyNoProxyProvidedInCR(t *testing.T) {
	_ = os.Setenv("HTTP_PROXY", "http://env-http:8080")
	_ = os.Setenv("HTTPS_PROXY", "https://env-https:8443")
	_ = os.Setenv("NO_PROXY", "env-no-proxy")
	defer func() {
		_ = os.Unsetenv("HTTP_PROXY")
		_ = os.Unsetenv("HTTPS_PROXY")
		_ = os.Unsetenv("NO_PROXY")
	}()

	mg := mustgatherv1alpha1.MustGather{
		ObjectMeta: metav1.ObjectMeta{Name: "mg", Namespace: "ns"},
		Spec: mustgatherv1alpha1.MustGatherSpec{
			ServiceAccountRef: v1.LocalObjectReference{Name: "sa"},
			UploadTarget:      sftpUploadTarget("case", "sec", true),
			ProxyConfig: mustgatherv1alpha1.ProxySpec{
				NoProxy: "cr-no-proxy",
			},
		},
	}

	job := getJobTemplate("img", mg)
	upload := findUploadContainerInJob(t, job)
	got := envValues(upload)

	if got[uploadEnvHttpProxy] != "http://env-http:8080" {
		t.Fatalf("expected %s from env, got %s", uploadEnvHttpProxy, got[uploadEnvHttpProxy])
	}
	if got[uploadEnvHttpsProxy] != "https://env-https:8443" {
		t.Fatalf("expected %s from env, got %s", uploadEnvHttpsProxy, got[uploadEnvHttpsProxy])
	}
	if got[uploadEnvNoProxy] != "env-no-proxy" {
		t.Fatalf("expected %s from env, got %s", uploadEnvNoProxy, got[uploadEnvNoProxy])
	}
}

func Test_getJobTemplate_NoFallbackWhenHttpAndHttpsProvidedInCR(t *testing.T) {
	_ = os.Setenv("HTTP_PROXY", "http://env-http:8080")
	_ = os.Setenv("HTTPS_PROXY", "https://env-https:8443")
	_ = os.Setenv("NO_PROXY", "env-no-proxy")
	defer func() {
		_ = os.Unsetenv("HTTP_PROXY")
		_ = os.Unsetenv("HTTPS_PROXY")
		_ = os.Unsetenv("NO_PROXY")
	}()

	mg := mustgatherv1alpha1.MustGather{
		ObjectMeta: metav1.ObjectMeta{Name: "mg", Namespace: "ns"},
		Spec: mustgatherv1alpha1.MustGatherSpec{
			ServiceAccountRef: v1.LocalObjectReference{Name: "sa"},
			UploadTarget:      sftpUploadTarget("case", "sec", true),
			ProxyConfig: mustgatherv1alpha1.ProxySpec{
				HTTPProxy:  "http://cr-http:8080",
				HTTPSProxy: "https://cr-https:8443",
				// NoProxy intentionally empty
			},
		},
	}

	job := getJobTemplate("img", mg)
	upload := findUploadContainerInJob(t, job)
	got := envValues(upload)

	if got[uploadEnvHttpProxy] != "http://cr-http:8080" {
		t.Fatalf("expected %s to be CR value, got %s", uploadEnvHttpProxy, got[uploadEnvHttpProxy])
	}
	if got[uploadEnvHttpsProxy] != "https://cr-https:8443" {
		t.Fatalf("expected %s to be CR value, got %s", uploadEnvHttpsProxy, got[uploadEnvHttpsProxy])
	}
	if _, ok := got[uploadEnvNoProxy]; ok {
		t.Fatalf("did not expect %s when CR NoProxy is empty", uploadEnvNoProxy)
	}
}

func Test_getJobTemplate_NoFallbackIfHttpsProvidedButHttpMissing(t *testing.T) {
	_ = os.Setenv("HTTP_PROXY", "http://env-http:8080")
	_ = os.Setenv("HTTPS_PROXY", "https://env-https:8443")
	_ = os.Setenv("NO_PROXY", "env-no-proxy")
	defer func() {
		_ = os.Unsetenv("HTTP_PROXY")
		_ = os.Unsetenv("HTTPS_PROXY")
		_ = os.Unsetenv("NO_PROXY")
	}()

	mg := mustgatherv1alpha1.MustGather{
		ObjectMeta: metav1.ObjectMeta{Name: "mg", Namespace: "ns"},
		Spec: mustgatherv1alpha1.MustGatherSpec{
			ServiceAccountRef: v1.LocalObjectReference{Name: "sa"},
			UploadTarget:      sftpUploadTarget("case", "sec", true),
			ProxyConfig: mustgatherv1alpha1.ProxySpec{
				HTTPSProxy: "https://cr-https:8443",
				// HTTPProxy empty to ensure fallback condition is false
			},
		},
	}

	job := getJobTemplate("img", mg)
	upload := findUploadContainerInJob(t, job)
	got := envValues(upload)

	// http proxy should not be present (no fallback)
	if _, ok := got[uploadEnvHttpProxy]; ok {
		t.Fatalf("did not expect %s when only HTTPS proxy is provided in CR", uploadEnvHttpProxy)
	}
	// https proxy should be from CR
	if got[uploadEnvHttpsProxy] != "https://cr-https:8443" {
		t.Fatalf("expected %s to be CR value, got %s", uploadEnvHttpsProxy, got[uploadEnvHttpsProxy])
	}
}
