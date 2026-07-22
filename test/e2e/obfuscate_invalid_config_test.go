//go:build e2e
// +build e2e

package e2e

import (
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	mustgatherv1alpha1 "github.com/openshift/must-gather-operator/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = ginkgo.Describe("MustGather resource", func() {
	ginkgo.Context("Obfuscation invalid ConfigMap validation", func() {
		var mustGatherName string
		var mustGatherCR *mustgatherv1alpha1.MustGather

		ginkgo.BeforeEach(func() {
			mustGatherName = fmt.Sprintf("mg-obfuscate-invalid-cm-%d", time.Now().UnixNano())
		})

		ginkgo.AfterEach(func() {
			if mustGatherCR != nil {
				ginkgo.By("Cleaning up MustGather CR")
				_ = nonAdminClient.Delete(testCtx, mustGatherCR)

				Eventually(func() bool {
					err := nonAdminClient.Get(testCtx, client.ObjectKey{
						Name:      mustGatherName,
						Namespace: ns.Name,
					}, &mustgatherv1alpha1.MustGather{})
					return apierrors.IsNotFound(err)
				}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(BeTrue())

				mustGatherCR = nil
			}
		})

		ginkgo.It("should fail with ObfuscationConfigInvalid when custom ConfigMap is missing [Skipped:Disconnected]", func() {
			ginkgo.By("Creating MustGather CR referencing a missing obfuscation ConfigMap")
			mustGatherCR = createObfuscateCustomConfigMustGather(
				mustGatherName, ns.Name, true, ObfuscationConfigMapValidName)

			fetched := waitForObfuscationConfigInvalidStatus(
				mustGatherName, ns.Name,
				obfuscationReasonConfigMapNotFound,
				ObfuscationConfigMapValidName,
			)
			ginkgo.GinkgoWriter.Printf(
				"MustGather failed with ObfuscationConfigInvalid: reason=%s message=%s\n",
				findStatusCondition(fetched, conditionObfuscationConfigInvalidE2E).Reason,
				fetched.Status.Reason)

			ginkgo.By("Verifying Job is not created")
			assertMustGatherJobNotCreated(mustGatherName, ns.Name)
		})

		ginkgo.It("should fail with ObfuscationConfigInvalid when ConfigMap lacks config.yaml key [Skipped:Disconnected]", func() {
			ginkgo.By("Seeding invalid obfuscation ConfigMap in operator namespace")
			seedObfuscationConfigMap(ObfuscateConfigMapInvalid)
			defer deleteObfuscationConfigMap(ObfuscateConfigMapInvalid)

			ginkgo.By("Creating MustGather CR referencing invalid obfuscation ConfigMap")
			mustGatherCR = createObfuscateCustomConfigMustGather(
				mustGatherName, ns.Name, true, ObfuscationConfigMapInvalidName)

			fetched := waitForObfuscationConfigInvalidStatus(
				mustGatherName, ns.Name,
				obfuscationReasonMissingConfigKey,
				obfuscateConfigMapKey,
			)
			ginkgo.GinkgoWriter.Printf(
				"MustGather failed with ObfuscationConfigInvalid: reason=%s message=%s\n",
				findStatusCondition(fetched, conditionObfuscationConfigInvalidE2E).Reason,
				fetched.Status.Reason,
			)

			ginkgo.By("Verifying Job is not created")
			assertMustGatherJobNotCreated(mustGatherName, ns.Name)
		})
	})
})
