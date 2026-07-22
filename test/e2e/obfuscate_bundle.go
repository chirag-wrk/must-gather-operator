//go:build e2e
// +build e2e

package e2e

import (
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/must-gather-operator/pkg/obfuscate"
)

const bundleVerifyPassMarker = "BUNDLE_VERIFY_PASS"

var (
	ipv4Pattern      = regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\b`)
	macPattern       = regexp.MustCompile(`\b(?:[0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}\b`)
	kindSecretPattern = regexp.MustCompile(`(?m)^kind:\s*Secret\s*$`)
	kindConfigMapPattern = regexp.MustCompile(`(?m)^kind:\s*ConfigMap\s*$`)
	ipTokenPattern   = regexp.MustCompile(`x-ipv4-\d+-x`)
)

// BundleVerifyOptions configures obfuscated bundle content checks (SC-001–SC-006).
type BundleVerifyOptions struct {
	// AllowMACCleartext skips MAC cleartext detection for SC-004 custom-policy bundles.
	AllowMACCleartext bool
}

// VerifyObfuscatedBundleRoot validates bundle directory content against default-policy expectations.
func VerifyObfuscatedBundleRoot(root string, opts BundleVerifyOptions) error {
	if err := assertObfuscationAuditPresent(root); err != nil {
		return err
	}
	if err := assertKubernetesSecretsOmitted(root); err != nil {
		return err
	}
	return assertSensitiveValuesObfuscated(root, opts)
}

func assertObfuscationAuditPresent(root string) error {
	hasAudit := false
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if base == obfuscate.ObfuscationLogFileName || base == obfuscate.ReportFileName {
			hasAudit = true
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walk bundle root: %w", err)
	}
	if !hasAudit {
		return fmt.Errorf("bundle missing %q or %q (SC-006)", obfuscate.ObfuscationLogFileName, obfuscate.ReportFileName)
	}
	return nil
}

func assertKubernetesSecretsOmitted(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !isYAMLFile(path) {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if kindSecretPattern.Match(content) {
			return fmt.Errorf("bundle contains Secret resource at %s (SC-002)", path)
		}
		if kindConfigMapPattern.Match(content) {
			return fmt.Errorf("bundle contains ConfigMap resource at %s (SC-002)", path)
		}
		return nil
	})
}

func assertSensitiveValuesObfuscated(root string, opts BundleVerifyOptions) error {
	var ipTokens []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(content)

		for _, ip := range ipv4Pattern.FindAllString(text, -1) {
			if isPreservedIP(ip) {
				continue
			}
			return fmt.Errorf("cleartext IPv4 %q found in %s (SC-001)", ip, path)
		}

		if !opts.AllowMACCleartext {
			for _, mac := range macPattern.FindAllString(text, -1) {
				return fmt.Errorf("cleartext MAC %q found in %s (SC-001)", mac, path)
			}
		}

		ipTokens = append(ipTokens, ipTokenPattern.FindAllString(text, -1)...)
		return nil
	})
	if err != nil {
		return err
	}

	if len(ipTokens) >= 2 && ipTokens[0] != ipTokens[1] {
		return fmt.Errorf("inconsistent IP tokens across bundle files: %v (SC-003)", ipTokens[:2])
	}
	return nil
}

func isYAMLFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}

func isPreservedIP(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	return parsed.IsLoopback() || parsed.Equal(net.IPv4zero)
}

// verifySFTPObfuscatedBundle downloads the uploaded tarball from SFTP, extracts it,
// and runs SC-001/SC-002/SC-006 bundle checks inside the verification pod.
//
// Manual fallback when CI cannot download from SFTP:
//  1. oc logs <sftp-bundle-verify-pod> -c sftp-bundle-verify -n <namespace>
//  2. Download the archive manually and run VerifyObfuscatedBundleRoot locally.
func verifySFTPObfuscatedBundle(namespace, secretName, host, caseID string, internalUser bool, opts BundleVerifyOptions) (bool, string, error) {
	verifyPodName := fmt.Sprintf("sftp-bundle-verify-%d", time.Now().UnixNano())
	httpProxy, httpsProxy, noProxy, _ := getOperatorProxyEnvVars()

	verifyPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      verifyPodName,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			RestartPolicy:      corev1.RestartPolicyNever,
			ServiceAccountName: serviceAccount,
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: func() *bool { b := true; return &b }(),
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Containers: []corev1.Container{
				{
					Name:    "sftp-bundle-verify",
					Image:   operatorImage,
					Command: []string{"/bin/bash", "-c", sftpBundleVerifyScript(host, internalUser, caseID, opts)},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: func() *bool { b := false; return &b }(),
						Capabilities: &corev1.Capabilities{
							Drop: []corev1.Capability{"ALL"},
						},
						RunAsNonRoot: func() *bool { b := true; return &b }(),
					},
					Env: buildVerifyPodEnvVars(secretName, httpProxy, httpsProxy, noProxy),
				},
			},
		},
	}

	if err := nonAdminClient.Create(testCtx, verifyPod); err != nil {
		return false, "", fmt.Errorf("failed to create bundle verification pod: %w", err)
	}

	Eventually(func() corev1.PodPhase {
		pod := &corev1.Pod{}
		if err := nonAdminClient.Get(testCtx, client.ObjectKey{
			Name:      verifyPodName,
			Namespace: namespace,
		}, pod); err != nil {
			return corev1.PodUnknown
		}
		return pod.Status.Phase
	}).WithTimeout(5*time.Minute).WithPolling(5*time.Second).Should(
		Or(Equal(corev1.PodSucceeded), Equal(corev1.PodFailed)),
		"bundle verification pod should complete")

	logs, err := getContainerLogs(namespace, verifyPodName, "sftp-bundle-verify")
	_ = nonAdminClient.Delete(testCtx, verifyPod)
	if err != nil {
		return false, logs, fmt.Errorf("failed to get bundle verification pod logs: %w", err)
	}

	return logsIndicateBundleVerifyPass(logs), logs, nil
}

func sftpBundleVerifyScript(host string, internalUser bool, caseID string, opts BundleVerifyOptions) string {
	listAndGet := fmt.Sprintf("ls -la %s_must-gather*.tar.gz", caseID)
	if internalUser {
		listAndGet = fmt.Sprintf("cd $SFTP_USERNAME\nls -la %s_must-gather*.tar.gz\nget %s_must-gather*.tar.gz", caseID, caseID)
	} else {
		listAndGet = fmt.Sprintf("ls -la %s_must-gather*.tar.gz\nget %s_must-gather*.tar.gz", caseID, caseID)
	}

	sftpHost := host
	if net.ParseIP(host) != nil && strings.Contains(host, ":") {
		sftpHost = "[" + host + "]"
	}

	macCheck := `
if grep -R -E '([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}' "$ROOT" >/dev/null 2>&1; then
  echo "BUNDLE_VERIFY_FAIL: cleartext MAC found (SC-001)"
  exit 1
fi
`
	if opts.AllowMACCleartext {
		macCheck = `
echo "BUNDLE_VERIFY: MAC cleartext allowed (SC-004 custom policy)"
`
	}

	return fmt.Sprintf(`
set -euo pipefail
mkdir -p /tmp/.ssh /tmp/bundle
touch /tmp/.ssh/known_hosts
chmod 700 /tmp/.ssh
chmod 600 /tmp/.ssh/known_hosts
cd /tmp/bundle

SSH_CONFIG="/tmp/.ssh/config"
echo "Host *" > ${SSH_CONFIG}
echo "  BatchMode no" >> ${SSH_CONFIG}
echo "  StrictHostKeyChecking no" >> ${SSH_CONFIG}
echo "  UserKnownHostsFile /tmp/.ssh/known_hosts" >> ${SSH_CONFIG}

if [ -n "${http_proxy}" ] || [ -n "${https_proxy}" ]; then
  PROXY_URL="${https_proxy:-${http_proxy}}"
  PROXY_NO_PROTOCOL=$(echo "${PROXY_URL}" | sed -E 's|^https?://||')
  if echo "${PROXY_NO_PROTOCOL}" | grep -q '@'; then
    PROXY_AUTH=$(echo "${PROXY_NO_PROTOCOL}" | sed -E 's|^([^@]+)@.*|\1|')
    PROXY_USER=$(echo "${PROXY_AUTH}" | cut -d: -f1)
    PROXY_PASSWORD=$(echo "${PROXY_AUTH}" | cut -d: -f2)
    PROXY_HOST_PORT=$(echo "${PROXY_NO_PROTOCOL}" | sed -E 's|^[^@]+@([^/]+).*|\1|')
  else
    PROXY_HOST_PORT=$(echo "${PROXY_NO_PROTOCOL}" | sed -E 's|^([^/]+).*|\1|')
    PROXY_USER=""
    PROXY_PASSWORD=""
  fi
  if [ -n "${PROXY_HOST_PORT}" ]; then
    if echo "${PROXY_URL}" | grep -q '^https://'; then
      export PROXY_HOST_PORT PROXY_USER PROXY_PASSWORD
      echo "  ProxyCommand https-proxy-connect-util %%h %%p" >> ${SSH_CONFIG}
    else
      if [ -n "${PROXY_USER}" ] && [ -n "${PROXY_PASSWORD}" ]; then
        echo "  ProxyCommand nc --proxy ${PROXY_HOST_PORT} --proxy-auth ${PROXY_USER}:${PROXY_PASSWORD} --proxy-type http %%h %%p" >> ${SSH_CONFIG}
      else
        echo "  ProxyCommand nc --proxy ${PROXY_HOST_PORT} --proxy-type http %%h %%p" >> ${SSH_CONFIG}
      fi
    fi
  fi
fi
chmod 600 ${SSH_CONFIG}

sshpass -e sftp -F ${SSH_CONFIG} $SFTP_USERNAME@%s << 'EOF'
%s
bye
EOF

ARCHIVE=$(ls %s_must-gather*.tar.gz | head -1)
mkdir -p extract
tar -xzf "$ARCHIVE" -C extract

ROOT=$(find extract -name '%s' -o -name '%s' | head -1 | xargs -r dirname)
if [ -z "$ROOT" ]; then
  echo "BUNDLE_VERIFY_FAIL: audit files not found after extract (SC-006)"
  exit 1
fi

if grep -R --include='*.yaml' --include='*.yml' -E '^kind: Secret' "$ROOT" >/dev/null 2>&1; then
  echo "BUNDLE_VERIFY_FAIL: Secret resource found (SC-002)"
  exit 1
fi
if grep -R --include='*.yaml' --include='*.yml' -E '^kind: ConfigMap' "$ROOT" >/dev/null 2>&1; then
  echo "BUNDLE_VERIFY_FAIL: ConfigMap resource found (SC-002)"
  exit 1
fi

if grep -R -E '(^|[^0-9])(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)([^0-9]|$)' "$ROOT" | grep -v '127\.0\.0\.1' | grep -v '0\.0\.0\.0' >/dev/null 2>&1; then
  echo "BUNDLE_VERIFY_FAIL: cleartext IPv4 found (SC-001)"
  exit 1
fi
%s
echo "%s"
`, sftpHost, listAndGet, caseID, obfuscate.ObfuscationLogFileName, obfuscate.ReportFileName, macCheck, bundleVerifyPassMarker)
}

func logsIndicateBundleVerifyPass(logs string) bool {
	return strings.Contains(logs, bundleVerifyPassMarker)
}
