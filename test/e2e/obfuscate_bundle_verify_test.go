//go:build e2e
// +build e2e

package e2e

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	mustgatherv1alpha1 "github.com/openshift/must-gather-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Bundle verification E2E (SC-001–SC-006):
// - Mode 1: download uploaded tarball from SFTP and assert obfuscated bundle content.
// - Mode 3: same bundle checks after source-PVC obfuscate + upload.
// - SC-004: custom ConfigMap with MAC obfuscation disabled allows cleartext MAC.
//
// Manual fallback when SFTP download is unavailable in CI:
//  1. oc logs <sftp-bundle-verify-pod> -c sftp-bundle-verify -n <namespace>
//  2. Download <caseID>_must-gather*.tar.gz manually from SFTP.
//  3. tar -xzf <archive> && go test -tags e2e ./test/e2e/ -run TestVerifyObfuscatedBundleRootFixture
//     or run VerifyObfuscatedBundleRoot against the extracted tree in a local harness.

var _ = ginkgo.Describe("MustGather resource", func() {
	ginkgo.Context("Obfuscation bundle content verification", func() {
		var mustGatherName string
		var mustGatherCR *mustgatherv1alpha1.MustGather

		ginkgo.BeforeEach(func() {
			mustGatherName = fmt.Sprintf("mg-obfuscate-bundle-e2e-%d", time.Now().UnixNano())
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

		ginkgo.It("should verify Mode 1 uploaded bundle meets SC-001 SC-002 SC-006 [Skipped:Disconnected]", func() {
			sftpUsername, sftpPassword, err := getCaseCreds()
			Expect(err).NotTo(HaveOccurred(), "Failed to get SFTP credentials")
			createCaseManagementSecret(caseManagementSecretNameValid, ns.Name, sftpUsername, sftpPassword)

			caseID := generateTestCaseID()
			mustGatherCR = createObfuscateMustGather(mustGatherName, ns.Name, serviceAccount, true, &MustGatherCROptions{
				UploadTarget: &UploadTargetOptions{
					CaseID:       caseID,
					SecretName:   caseManagementSecretNameValid,
					InternalUser: false,
					Host:         prodHostName,
				},
			})

			_, mustGatherPod := waitForMustGatherJobSuccess(mustGatherName, ns.Name)
			uploadLogs, err := getContainerLogs(ns.Name, mustGatherPod.Name, uploadContainerName)
			Expect(err).NotTo(HaveOccurred())
			Expect(uploadLogs).To(ContainSubstring(obfuscateLogCompleteMarker))

			found, verifyLogs, err := verifySFTPObfuscatedBundle(
				ns.Name, caseManagementSecretNameValid, prodHostName, caseID, false, BundleVerifyOptions{})
			if err != nil {
				ginkgo.GinkgoWriter.Printf("SFTP bundle verification error: %v\n", err)
			}
			ginkgo.GinkgoWriter.Printf("SFTP bundle verification logs:\n%s\n", verifyLogs)
			Expect(found).To(BeTrue(),
				"Mode 1 uploaded bundle should pass SC-001/SC-002/SC-006 checks for caseID %s", caseID)
		})

		ginkgo.It("should verify Mode 3 source PVC uploaded bundle content [Skipped:Disconnected]", func() {
			ginkgo.By("Creating PersistentVolumeClaim for source obfuscation bundle verify")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "must-gather-pvc.yaml"), ns.Name)
			Eventually(func() error {
				return nonAdminClient.Get(testCtx, client.ObjectKey{
					Name:      mustGatherPVCName,
					Namespace: ns.Name,
				}, &corev1.PersistentVolumeClaim{})
			}).WithTimeout(3 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
			defer func() {
				loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "must-gather-pvc.yaml"), ns.Name)
			}()

			seedObfuscateSourcePVC(ns.Name, mustGatherPVCName, obfuscateSourceBundleSubPath)

			sftpUsername, sftpPassword, err := getCaseCreds()
			Expect(err).NotTo(HaveOccurred())
			createCaseManagementSecret(caseManagementSecretNameValid, ns.Name, sftpUsername, sftpPassword)

			caseID := generateTestCaseID()
			mustGatherCR = createObfuscateSourceMustGather(
				mustGatherName, ns.Name, true, obfuscateSourceBundleSubPath, &UploadTargetOptions{
					CaseID:       caseID,
					SecretName:   caseManagementSecretNameValid,
					InternalUser: false,
					Host:         prodHostName,
				})

			waitForMustGatherJobSuccess(mustGatherName, ns.Name)

			found, verifyLogs, err := verifySFTPObfuscatedBundle(
				ns.Name, caseManagementSecretNameValid, prodHostName, caseID, false, BundleVerifyOptions{})
			if err != nil {
				ginkgo.GinkgoWriter.Printf("Mode 3 bundle verification error: %v\n", err)
			}
			ginkgo.GinkgoWriter.Printf("Mode 3 bundle verification logs:\n%s\n", verifyLogs)
			Expect(found).To(BeTrue(), "Mode 3 uploaded bundle should pass content checks for caseID %s", caseID)
		})

		ginkgo.It("should allow cleartext MAC with SC-004 custom policy ConfigMap [Skipped:Disconnected]", func() {
			ginkgo.By("Creating PVC and seeding source bundle with cleartext MAC")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "must-gather-pvc.yaml"), ns.Name)
			Eventually(func() error {
				return nonAdminClient.Get(testCtx, client.ObjectKey{
					Name:      mustGatherPVCName,
					Namespace: ns.Name,
				}, &corev1.PersistentVolumeClaim{})
			}).WithTimeout(3 * time.Minute).WithPolling(5 * time.Second).Should(Succeed())
			defer func() {
				deleteObfuscationConfigMap(ObfuscateConfigMapMACDisabled)
				loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "must-gather-pvc.yaml"), ns.Name)
			}()

			seedObfuscateSourcePVC(ns.Name, mustGatherPVCName, obfuscateSourceBundleSubPath)
			seedObfuscationConfigMap(ObfuscateConfigMapMACDisabled)

			sftpUsername, sftpPassword, err := getCaseCreds()
			Expect(err).NotTo(HaveOccurred())
			createCaseManagementSecret(caseManagementSecretNameValid, ns.Name, sftpUsername, sftpPassword)

			caseID := generateTestCaseID()
			mustGatherCR = createObfuscateMustGather(mustGatherName, ns.Name, serviceAccount, true, &MustGatherCROptions{
				Obfuscate: &ObfuscateOptions{
					ConfigMapRefName: ObfuscationConfigMapMACDisabledName,
					SourcePVCName:    mustGatherPVCName,
					SourceSubPath:    obfuscateSourceBundleSubPath,
				},
				UploadTarget: &UploadTargetOptions{
					CaseID:       caseID,
					SecretName:   caseManagementSecretNameValid,
					InternalUser: false,
					Host:         prodHostName,
				},
			})

			waitForMustGatherJobSuccess(mustGatherName, ns.Name)

			found, verifyLogs, err := verifySFTPObfuscatedBundle(
				ns.Name, caseManagementSecretNameValid, prodHostName, caseID, false,
				BundleVerifyOptions{AllowMACCleartext: true})
			if err != nil {
				ginkgo.GinkgoWriter.Printf("SC-004 bundle verification error: %v\n", err)
			}
			ginkgo.GinkgoWriter.Printf("SC-004 bundle verification logs:\n%s\n", verifyLogs)
			Expect(found).To(BeTrue(),
				"SC-004 custom policy bundle should pass with MAC cleartext allowed for caseID %s", caseID)
		})
	})
})
