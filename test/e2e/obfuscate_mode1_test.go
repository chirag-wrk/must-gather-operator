//go:build e2e
// +build e2e

package e2e

import (
	"fmt"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	mustgatherv1alpha1 "github.com/openshift/must-gather-operator/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = ginkgo.Describe("MustGather resource", func() {
	ginkgo.Context("Obfuscation Mode 1 gather obfuscate upload", func() {
		var mustGatherName string
		var mustGatherCR *mustgatherv1alpha1.MustGather

		ginkgo.BeforeEach(func() {
			mustGatherName = fmt.Sprintf("mg-obfuscate-mode1-e2e-%d", time.Now().UnixNano())
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

		ginkgo.It("should gather, obfuscate with default policy, and upload to SFTP [Skipped:Disconnected]", func() {
			ginkgo.By("Getting SFTP credentials")
			sftpUsername, sftpPassword, err := getCaseCreds()
			Expect(err).NotTo(HaveOccurred(), "Failed to get SFTP credentials")

			ginkgo.By("Creating case-management secret")
			createCaseManagementSecret(caseManagementSecretNameValid, ns.Name, sftpUsername, sftpPassword)

			caseID := generateTestCaseID()
			ginkgo.GinkgoWriter.Printf("Using unique caseID: %s\n", caseID)

			ginkgo.By("Creating MustGather CR with obfuscate.enabled and default policy upload (Mode 1)")
			mustGatherCR = createObfuscateMustGather(mustGatherName, ns.Name, serviceAccount, true, &MustGatherCROptions{
				UploadTarget: &UploadTargetOptions{
					CaseID:       caseID,
					SecretName:   caseManagementSecretNameValid,
					InternalUser: false,
					Host:         prodHostName,
				},
			})

			fetchedMG := &mustgatherv1alpha1.MustGather{}
			err = nonAdminClient.Get(testCtx, client.ObjectKey{
				Name:      mustGatherName,
				Namespace: ns.Name,
			}, fetchedMG)
			Expect(err).NotTo(HaveOccurred(), "Failed to get MustGather CR")
			Expect(fetchedMG.Spec.Obfuscate).NotTo(BeNil())
			Expect(fetchedMG.Spec.Obfuscate.Enabled).NotTo(BeNil())
			Expect(*fetchedMG.Spec.Obfuscate.Enabled).To(BeTrue())
			Expect(fetchedMG.Spec.Obfuscate.ObfuscationConfigRef).To(BeNil(), "Mode 1 default policy should not set custom ConfigMap ref")
			Expect(fetchedMG.Spec.UploadTarget).NotTo(BeNil())

			job, mustGatherPod := waitForMustGatherJobSuccess(mustGatherName, ns.Name)

			gatherContainer := findJobContainer(job, gatherContainerName)
			Expect(gatherContainer).NotTo(BeNil(), "Mode 1 Job should include gather container")
			uploadContainer := findJobContainer(job, uploadContainerName)
			Expect(uploadContainer).NotTo(BeNil(), "Mode 1 Job should include upload container")

			uploadEnv := containerEnvMap(uploadContainer)
			Expect(uploadEnv[uploadEnvObfuscateName]).To(Equal("true"), "upload container should set obfuscate=true")
			Expect(uploadEnv[uploadEnvObfuscateConfigName]).To(Equal(defaultObfuscateConfigPath),
				"default policy should use embedded obfuscate config path")

			Expect(mustGatherPod).NotTo(BeNil())
			podContainerNames := make(map[string]bool)
			for _, c := range mustGatherPod.Spec.Containers {
				podContainerNames[c.Name] = true
			}
			Expect(podContainerNames).To(HaveKey(gatherContainerName))
			Expect(podContainerNames).To(HaveKey(uploadContainerName))

			ginkgo.By("Verifying upload container logs contain obfuscation hook markers")
			uploadLogs, err := getContainerLogs(ns.Name, mustGatherPod.Name, uploadContainerName)
			Expect(err).NotTo(HaveOccurred(), "Failed to get upload container logs")
			Expect(uploadLogs).To(ContainSubstring(obfuscateLogRunningMarker),
				"upload logs should include Running obfuscation marker")
			Expect(uploadLogs).To(ContainSubstring(obfuscateLogCompleteMarker),
				"upload logs should include Obfuscation complete marker")
			ginkgo.GinkgoWriter.Printf("Upload container obfuscation log excerpt:\n%s\n",
				obfuscateLogExcerpt(uploadLogs))

			ginkgo.By("Verifying obfuscated bundle uploaded to SFTP")
			found, sftpLogs, err := verifySFTPUpload(ns.Name, caseManagementSecretNameValid, prodHostName, caseID, false)
			if err != nil {
				ginkgo.GinkgoWriter.Printf("SFTP verification error: %v\n", err)
			}
			ginkgo.GinkgoWriter.Printf("SFTP directory listing:\n%s\n", sftpLogs)
			Expect(found).To(BeTrue(),
				"File with caseID %s should exist on SFTP server after Mode 1 obfuscate upload", caseID)
		})
	})
})

func obfuscateLogExcerpt(logs string) string {
	lines := strings.Split(logs, "\n")
	var excerpt []string
	for _, line := range lines {
		if strings.Contains(line, obfuscateLogRunningMarker) || strings.Contains(line, obfuscateLogCompleteMarker) {
			excerpt = append(excerpt, line)
		}
	}
	if len(excerpt) == 0 {
		return "<obfuscation markers not found in log lines>"
	}
	return strings.Join(excerpt, "\n")
}
