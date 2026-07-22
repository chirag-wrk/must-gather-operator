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

var _ = ginkgo.Describe("MustGather resource", func() {
	ginkgo.Context("Obfuscation Mode 2/3 source PVC", func() {
		var mustGatherName string
		var mustGatherCR *mustgatherv1alpha1.MustGather

		ginkgo.BeforeEach(func() {
			mustGatherName = fmt.Sprintf("mg-obfuscate-source-e2e-%d", time.Now().UnixNano())

			ginkgo.By("Creating PersistentVolumeClaim for source obfuscation tests")
			loader.CreateFromFile(testassets.ReadFile, filepath.Join("testdata", "must-gather-pvc.yaml"), ns.Name)

			pvc := &corev1.PersistentVolumeClaim{}
			Eventually(func() error {
				return nonAdminClient.Get(testCtx, client.ObjectKey{
					Name:      mustGatherPVCName,
					Namespace: ns.Name,
				}, pvc)
			}).WithTimeout(3*time.Minute).WithPolling(5*time.Second).Should(Succeed())

			ginkgo.By("Seeding source PVC with a minimal must-gather bundle")
			seedObfuscateSourcePVC(ns.Name, mustGatherPVCName, obfuscateSourceBundleSubPath)
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

			ginkgo.By("Cleaning up PVC")
			loader.DeleteFromFile(testassets.ReadFile, filepath.Join("testdata", "must-gather-pvc.yaml"), ns.Name)
			Eventually(func() bool {
				err := nonAdminClient.Get(testCtx, client.ObjectKey{
					Name:      mustGatherPVCName,
					Namespace: ns.Name,
				}, &corev1.PersistentVolumeClaim{})
				return apierrors.IsNotFound(err)
			}).WithTimeout(2*time.Minute).WithPolling(5*time.Second).Should(BeTrue())
		})

		ginkgo.It("should run Mode 2 source obfuscate without gather or SFTP upload [Skipped:Disconnected]", func() {
			sourceMarker := readSourcePVCMarker(ns.Name, mustGatherPVCName, obfuscateSourceBundleSubPath)
			Expect(sourceMarker).To(Equal("obfuscate-e2e-source-marker"))

			ginkgo.By("Creating MustGather CR with source PVC obfuscation only (Mode 2)")
			mustGatherCR = createObfuscateSourceMustGather(
				mustGatherName, ns.Name, true, obfuscateSourceBundleSubPath, nil)

			job, mustGatherPod := waitForMustGatherJobSuccess(mustGatherName, ns.Name)
			assertObfuscateSourceModeJobShape(job, obfuscateSourceBundleSubPath)

			uploadLogs, err := getContainerLogs(ns.Name, mustGatherPod.Name, uploadContainerName)
			Expect(err).NotTo(HaveOccurred(), "Failed to get upload container logs")
			Expect(uploadLogs).To(ContainSubstring(obfuscateLogRunningMarker))
			Expect(uploadLogs).To(ContainSubstring(obfuscateLogCompleteMarker))
			Expect(uploadLogs).To(ContainSubstring(obfuscateLogNoUploadMarker))

			ginkgo.By("Verifying source PVC marker unchanged after obfuscation")
			Expect(readSourcePVCMarker(ns.Name, mustGatherPVCName, obfuscateSourceBundleSubPath)).
				To(Equal(sourceMarker), "source PVC bundle must remain unchanged (read-only)")
		})

		ginkgo.It("should run Mode 3 source obfuscate and upload to SFTP [Skipped:Disconnected]", func() {
			sftpUsername, sftpPassword, err := getCaseCreds()
			Expect(err).NotTo(HaveOccurred(), "Failed to get SFTP credentials")
			createCaseManagementSecret(caseManagementSecretNameValid, ns.Name, sftpUsername, sftpPassword)

			caseID := generateTestCaseID()
			mustGatherCR = createObfuscateSourceMustGather(
				mustGatherName, ns.Name, true, obfuscateSourceBundleSubPath, &UploadTargetOptions{
					CaseID:       caseID,
					SecretName:   caseManagementSecretNameValid,
					InternalUser: false,
					Host:         prodHostName,
				})

			job, mustGatherPod := waitForMustGatherJobSuccess(mustGatherName, ns.Name)
			assertObfuscateSourceModeJobShape(job, obfuscateSourceBundleSubPath)

			uploadLogs, err := getContainerLogs(ns.Name, mustGatherPod.Name, uploadContainerName)
			Expect(err).NotTo(HaveOccurred())
			Expect(uploadLogs).To(ContainSubstring(obfuscateLogRunningMarker))
			Expect(uploadLogs).To(ContainSubstring(obfuscateLogCompleteMarker))

			found, sftpLogs, err := verifySFTPUpload(ns.Name, caseManagementSecretNameValid, prodHostName, caseID, false)
			if err != nil {
				ginkgo.GinkgoWriter.Printf("SFTP verification error: %v\n", err)
			}
			ginkgo.GinkgoWriter.Printf("SFTP directory listing:\n%s\n", sftpLogs)
			Expect(found).To(BeTrue(), "Mode 3 should upload obfuscated bundle for caseID %s", caseID)
		})

		ginkgo.It("should isolate sequential source PVC obfuscate runs [Skipped:Disconnected]", func() {
			sourceMarker := readSourcePVCMarker(ns.Name, mustGatherPVCName, obfuscateSourceBundleSubPath)

			name1 := mustGatherName
			mg1 := createObfuscateSourceMustGather(name1, ns.Name, true, obfuscateSourceBundleSubPath, nil)
			job1, _ := waitForMustGatherJobSuccess(name1, ns.Name)
			assertObfuscateSourceModeJobShape(job1, obfuscateSourceBundleSubPath)

			_ = nonAdminClient.Delete(testCtx, mg1)
			Eventually(func() bool {
				err := nonAdminClient.Get(testCtx, client.ObjectKey{Name: name1, Namespace: ns.Name}, &mustgatherv1alpha1.MustGather{})
				return apierrors.IsNotFound(err)
			}).WithTimeout(2 * time.Minute).WithPolling(5 * time.Second).Should(BeTrue())

			name2 := fmt.Sprintf("mg-obfuscate-source-seq2-%d", time.Now().UnixNano())
			mustGatherCR = createObfuscateSourceMustGather(name2, ns.Name, true, obfuscateSourceBundleSubPath, nil)
			job2, _ := waitForMustGatherJobSuccess(name2, ns.Name)
			assertObfuscateSourceModeJobShape(job2, obfuscateSourceBundleSubPath)

			ginkgo.By("Verifying both Jobs used the same read-only source subPath")
			upload1 := findJobContainer(job1, uploadContainerName)
			upload2 := findJobContainer(job2, uploadContainerName)
			mount1 := volumeMountForContainer(upload1, obfuscateSourceVolumeNameE2E)
			mount2 := volumeMountForContainer(upload2, obfuscateSourceVolumeNameE2E)
			Expect(mount1.SubPath).To(Equal(obfuscateSourceBundleSubPath))
			Expect(mount2.SubPath).To(Equal(obfuscateSourceBundleSubPath))
			Expect(mount1.SubPath).To(Equal(mount2.SubPath))

			ginkgo.By("Verifying source PVC marker unchanged after sequential runs")
			Expect(readSourcePVCMarker(ns.Name, mustGatherPVCName, obfuscateSourceBundleSubPath)).
				To(Equal(sourceMarker), "sequential obfuscate runs must not modify source PVC content")
		})
	})
})
