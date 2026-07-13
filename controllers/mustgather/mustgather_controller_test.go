package mustgather

import (
	"context"
	"strings"
	"testing"

	mustgatherv1alpha1 "github.com/openshift/must-gather-operator/api/v1alpha1"
	"github.com/redhat-cop/operator-utils/pkg/util"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func generateFakeClient(objs ...runtime.Object) (client.Client, *runtime.Scheme) {
	s := scheme.Scheme
	s.AddKnownTypes(mustgatherv1alpha1.GroupVersion, &mustgatherv1alpha1.MustGather{})
	cl := fake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects(objs...).
		WithStatusSubresource(&mustgatherv1alpha1.MustGather{}).
		Build()
	return cl, s
}

func newTestReconciler(t *testing.T, objs ...runtime.Object) (*MustGatherReconciler, client.Client) {
	t.Helper()
	cl, s := generateFakeClient(objs...)
	r := &MustGatherReconciler{
		ReconcilerBase: util.NewReconcilerBase(cl, s, nil, record.NewFakeRecorder(10), nil),
	}
	return r, cl
}

func readyMustGather() *mustgatherv1alpha1.MustGather {
	mg := createMustGatherObject()
	mg.Spec.ServiceAccountRef.Name = "default"
	mg.SetFinalizers([]string{mustGatherFinalizer})
	return mg
}

func findCondition(conditions []metav1.Condition, conditionType string) (metav1.Condition, bool) {
	for _, c := range conditions {
		if c.Type == conditionType {
			return c, true
		}
	}
	return metav1.Condition{}, false
}

func Test_validateReconcilePrerequisites_EmptyEnv(t *testing.T) {
	tests := []struct {
		name          string
		gatherImage   string
		operatorImage string
		uploadEnabled bool
		wantType      string
		wantReason    string
	}{
		{
			name:          "missing gather image for upload CR",
			gatherImage:   "",
			operatorImage: "quay.io/test/operator:latest",
			uploadEnabled: true,
			wantType:      ConditionUploadOperatorConfigInvalid,
			wantReason:    "MissingGatherImage",
		},
		{
			name:          "missing operator image for upload CR",
			gatherImage:   "quay.io/test/must-gather:latest",
			operatorImage: "",
			uploadEnabled: true,
			wantType:      ConditionUploadOperatorConfigInvalid,
			wantReason:    "MissingOperatorImage",
		},
		{
			name:          "missing gather image for gather-only CR",
			gatherImage:   "",
			operatorImage: "",
			uploadEnabled: false,
			wantType:      ConditionUploadOperatorConfigInvalid,
			wantReason:    "MissingGatherImage",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(defaultMustGatherImageEnv, tt.gatherImage)
			t.Setenv(operatorImageEnv, tt.operatorImage)

			mg := readyMustGather()
			if !tt.uploadEnabled {
				mg.Spec.UploadTarget = nil
			}

			gotType, gotReason, gotMessage, ok := validateReconcilePrerequisites(mg)
			if ok {
				t.Fatal("expected validation failure")
			}
			if gotType != tt.wantType {
				t.Fatalf("condition type: got %q, want %q", gotType, tt.wantType)
			}
			if gotReason != tt.wantReason {
				t.Fatalf("reason: got %q, want %q", gotReason, tt.wantReason)
			}
			if gotMessage == "" {
				t.Fatal("expected non-empty validation message")
			}
		})
	}
}

func Test_validateReconcilePrerequisites_UploadConfigurationInvalid(t *testing.T) {
	t.Setenv(defaultMustGatherImageEnv, "quay.io/test/must-gather:latest")
	t.Setenv(operatorImageEnv, "quay.io/test/operator:latest")

	mg := readyMustGather()
	mg.Spec.UploadTarget.SFTP.CaseManagementAccountSecretRef.Name = ""

	gotType, gotReason, _, ok := validateReconcilePrerequisites(mg)
	if ok {
		t.Fatal("expected validation failure")
	}
	if gotType != ConditionUploadConfigurationInvalid {
		t.Fatalf("condition type: got %q, want %q", gotType, ConditionUploadConfigurationInvalid)
	}
	if gotReason != "MissingSecretRef" {
		t.Fatalf("reason: got %q, want MissingSecretRef", gotReason)
	}
}

func TestReconcile_UploadOperatorConfigInvalid_MissingGatherImage(t *testing.T) {
	t.Setenv(defaultMustGatherImageEnv, "")
	t.Setenv(operatorImageEnv, "quay.io/test/operator:latest")

	mg := readyMustGather()
	r, cl := newTestReconciler(t, mg)

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: mg.Name, Namespace: mg.Namespace},
	})
	if err == nil {
		t.Fatal("expected reconcile error")
	}

	updated := &mustgatherv1alpha1.MustGather{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: mg.Name, Namespace: mg.Namespace}, updated); err != nil {
		t.Fatalf("get MustGather: %v", err)
	}

	cond, ok := findCondition(updated.Status.Conditions, ConditionUploadOperatorConfigInvalid)
	if !ok {
		t.Fatalf("expected %s condition", ConditionUploadOperatorConfigInvalid)
	}
	if cond.Reason != "MissingGatherImage" {
		t.Fatalf("reason: got %q, want MissingGatherImage", cond.Reason)
	}
	if !strings.Contains(cond.Message, defaultMustGatherImageEnv) {
		t.Fatalf("message should reference %s, got %q", defaultMustGatherImageEnv, cond.Message)
	}
}

func TestReconcile_UploadOperatorConfigInvalid_MissingOperatorImage(t *testing.T) {
	t.Setenv(defaultMustGatherImageEnv, "quay.io/test/must-gather:latest")
	t.Setenv(operatorImageEnv, "")

	mg := readyMustGather()
	r, cl := newTestReconciler(t, mg)

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: mg.Name, Namespace: mg.Namespace},
	})
	if err == nil {
		t.Fatal("expected reconcile error")
	}

	updated := &mustgatherv1alpha1.MustGather{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: mg.Name, Namespace: mg.Namespace}, updated); err != nil {
		t.Fatalf("get MustGather: %v", err)
	}

	cond, ok := findCondition(updated.Status.Conditions, ConditionUploadOperatorConfigInvalid)
	if !ok {
		t.Fatalf("expected %s condition", ConditionUploadOperatorConfigInvalid)
	}
	if cond.Reason != "MissingOperatorImage" {
		t.Fatalf("reason: got %q, want MissingOperatorImage", cond.Reason)
	}
	if !strings.Contains(cond.Message, operatorImageEnv) {
		t.Fatalf("message should reference %s, got %q", operatorImageEnv, cond.Message)
	}
}

func TestReconcile_UploadConfigurationInvalid_MissingSecretRef(t *testing.T) {
	t.Setenv(defaultMustGatherImageEnv, "quay.io/test/must-gather:latest")
	t.Setenv(operatorImageEnv, "quay.io/test/operator:latest")

	mg := readyMustGather()
	mg.Spec.UploadTarget.SFTP.CaseManagementAccountSecretRef.Name = ""
	r, cl := newTestReconciler(t, mg)

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: mg.Name, Namespace: mg.Namespace},
	})
	if err == nil {
		t.Fatal("expected reconcile error")
	}

	updated := &mustgatherv1alpha1.MustGather{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: mg.Name, Namespace: mg.Namespace}, updated); err != nil {
		t.Fatalf("get MustGather: %v", err)
	}

	cond, ok := findCondition(updated.Status.Conditions, ConditionUploadConfigurationInvalid)
	if !ok {
		t.Fatalf("expected %s condition", ConditionUploadConfigurationInvalid)
	}
	if cond.Reason != "MissingSecretRef" {
		t.Fatalf("reason: got %q, want MissingSecretRef", cond.Reason)
	}
}

func TestReconcile_UploadCredentialsInvalid_SecretNotFound(t *testing.T) {
	t.Setenv(defaultMustGatherImageEnv, "quay.io/test/must-gather:latest")
	t.Setenv(operatorImageEnv, "quay.io/test/operator:latest")

	mg := readyMustGather()
	r, cl := newTestReconciler(t, mg)

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: mg.Name, Namespace: mg.Namespace},
	})
	if err == nil {
		t.Fatal("expected reconcile error")
	}

	updated := &mustgatherv1alpha1.MustGather{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: mg.Name, Namespace: mg.Namespace}, updated); err != nil {
		t.Fatalf("get MustGather: %v", err)
	}

	cond, ok := findCondition(updated.Status.Conditions, ConditionUploadCredentialsInvalid)
	if !ok {
		t.Fatalf("expected %s condition", ConditionUploadCredentialsInvalid)
	}
	if cond.Reason != "SecretNotFound" {
		t.Fatalf("reason: got %q, want SecretNotFound", cond.Reason)
	}
	if !strings.Contains(cond.Message, "case-management-creds") {
		t.Fatalf("message should reference secret name, got %q", cond.Message)
	}
}

func TestReconcile_UploadJobFailed(t *testing.T) {
	t.Setenv(defaultMustGatherImageEnv, "quay.io/test/must-gather:latest")
	t.Setenv(operatorImageEnv, "quay.io/test/operator:latest")

	mg := readyMustGather()
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mg.Name,
			Namespace: defaultMustGatherNamespace,
		},
		Status: batchv1.JobStatus{
			Failed: 1,
		},
	}

	r, cl := newTestReconciler(t, mg, job)

	_, err := r.Reconcile(context.Background(), reconcile.Request{
		NamespacedName: types.NamespacedName{Name: mg.Name, Namespace: mg.Namespace},
	})
	if err == nil {
		t.Fatal("expected reconcile error from job failure handling")
	}

	updated := &mustgatherv1alpha1.MustGather{}
	if err := cl.Get(context.Background(), types.NamespacedName{Name: mg.Name, Namespace: mg.Namespace}, updated); err != nil {
		t.Fatalf("get MustGather: %v", err)
	}

	cond, ok := findCondition(updated.Status.Conditions, ConditionUploadJobFailed)
	if !ok {
		t.Fatalf("expected %s condition", ConditionUploadJobFailed)
	}
	if cond.Reason != "JobFailed" {
		t.Fatalf("reason: got %q, want JobFailed", cond.Reason)
	}
	if !strings.Contains(cond.Message, mg.Name) {
		t.Fatalf("message should reference job name, got %q", cond.Message)
	}
}

func TestMustGatherController(t *testing.T) {
	t.Setenv(defaultMustGatherImageEnv, "quay.io/test/must-gather:latest")
	t.Setenv(operatorImageEnv, "quay.io/test/operator:latest")

	mgObj := createMustGatherObject()
	secObj := createMustGatherSecretObject()

	objs := []runtime.Object{
		mgObj,
		secObj,
	}

	cl, s := generateFakeClient(objs...)

	r := MustGatherReconciler{
		ReconcilerBase: util.NewReconcilerBase(cl, s, nil, record.NewFakeRecorder(10), nil),
	}

	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      mgObj.Name,
			Namespace: mgObj.Namespace,
		},
	}

	res, err := r.Reconcile(context.TODO(), req)
	if err != nil {
		t.Fatalf("reconcile: (%v)", err)
	}

	if res != (reconcile.Result{}) {
		t.Error("reconcile did not return an empty Result")
	}
}

func createMustGatherObject() *mustgatherv1alpha1.MustGather {
	return &mustgatherv1alpha1.MustGather{
		TypeMeta: metav1.TypeMeta{
			Kind: "MustGather",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-must-gather",
			Namespace: "must-gather-operator",
		},
		Spec: mustgatherv1alpha1.MustGatherSpec{
			UploadTarget: &mustgatherv1alpha1.UploadTarget{
				Type: mustgatherv1alpha1.UploadTypeSFTP,
				SFTP: &mustgatherv1alpha1.SFTPUploadTargetConfig{
					CaseID: "01234567",
					CaseManagementAccountSecretRef: corev1.LocalObjectReference{
						Name: "case-management-creds",
					},
				},
			},
			ServiceAccountRef: corev1.LocalObjectReference{
				Name: "",
			},
		},
	}
}

func createMustGatherSecretObject() *corev1.Secret {
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "case-management-creds",
			Namespace: "must-gather-operator",
		},
		Data: map[string][]byte{
			"username": []byte("somefakeuser"),
			"password": []byte("somefakepassword"),
		},
	}
}
