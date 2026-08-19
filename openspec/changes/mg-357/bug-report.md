# Bug Context Report: Upload/obfuscation proceeds after gather exits without success verification

**Bug ID**: MG-357

**Severity**: Major

**Priority**: Major

**Created**: 2026-08-19

**Status**: Draft

**Linked Epic**: MG-297 — Integrate must-gather-clean with must-gather-operator

**Input**: Jira Bug ticket: "Fix Must-gather upload runs after gather process exits without verifying gather succeeded, allowing partial/failed bundles to be obfuscated and uploaded"

## Bug Description

When a MustGather Job includes an upload container (SFTP upload and/or obfuscation enabled), the upload container waits only for the absence of a running `gather` process via `pgrep -a gather`. It does not verify that gather completed successfully. After five consecutive 30-second intervals with no matching process, the upload container unconditionally invokes `/usr/local/bin/upload`, which may obfuscate and upload partial or failed gather output from the shared `/must-gather` volume.

Separately, the default gather shell command can mask gather failures: without `pipefail`, the exit status reflects the `tee` pipeline stage rather than the gather binary, and non-timeout failures are not explicitly propagated with `exit $status`.

## Steps to Reproduce

1. Deploy the Must-Gather Operator on an OpenShift cluster with `DEFAULT_MUST_GATHER_IMAGE` and `OPERATOR_IMAGE` configured. Create namespace `must-gather-test` and a ServiceAccount with cluster read access (e.g., `must-gather-admin`). Create an SFTP credentials Secret if testing upload (`case-management-creds`).

2. Apply a MustGather CR with upload and obfuscation enabled and a gather command that fails immediately [INFERRED — addresses Stage 0 specificity gap]:

```yaml
apiVersion: operator.openshift.io/v1
kind: MustGather
metadata:
  name: gather-fail-repro
  namespace: must-gather-test
spec:
  serviceAccountName: must-gather-admin
  obfuscate:
    enabled: true
  gatherSpec:
    command: ["/bin/sh", "-c", "echo 'simulated gather failure'; exit 1"]
  uploadTarget:
    type: SFTP
    sftp:
      caseID: "00000000"
      caseManagementAccountSecretRef:
        name: case-management-creds
      internalUser: true
```

   **PVC-only variant** (no SFTP): omit `uploadTarget`, add `storage` with a PVC reference per `examples/mustgather_obfuscate_gather_pvc.yaml`.

3. Wait for the operator to create the Job and Pods. Identify the upload container:

```bash
oc get job -n must-gather-test -l must-gather=gather-fail-repro
oc logs -n must-gather-test job/<job-name> -c upload -f
```

4. Observe upload container behavior after gather exits:
   - Upload logs show the `pgrep` debounce loop completing (`no gather is running (N / 4)`)
   - `/usr/local/bin/upload` runs obfuscation and/or SFTP upload despite gather failure

5. Verify Job and MustGather status and artifacts:

```bash
oc get mustgather gather-fail-repro -n must-gather-test -o yaml
oc get job -n must-gather-test -l must-gather=gather-fail-repro
oc logs -n must-gather-test job/<job-name> -c gather
```

   Check whether the Job/MustGather CR reports `Completed` while gather logs show a non-zero exit, and whether obfuscated output was written to PVC or uploaded to SFTP.

## Expected Behavior

Upload and obfuscation must run only after gather completes **successfully**. When gather fails or produces incomplete output:

- The upload container must not run obfuscation or SFTP upload
- The Kubernetes Job must fail
- The MustGather CR status must be `Failed` (not `Completed`)
- No bundle must be uploaded to SFTP
- With `obfuscate.enabled` and PVC storage (no SFTP), no cleaned/obfuscated output must be written to the PVC

When gather succeeds, existing flows must continue to work: gather → obfuscate (if enabled) → upload (if configured). `obfuscate.source` (upload-only, no gather) behavior must remain unchanged.

## Actual Behavior

The upload container treats gather process termination as completion. After the debounce loop in `uploadCommand`, it invokes `/usr/local/bin/upload` unconditionally. Obfuscation and/or SFTP upload proceed on whatever content exists on the shared volume, including partial or failed gather output.

The gather container may exit 0 even when gather fails, because the default `gatherCommand` captures `$?` after a `tee` pipeline without `set -o pipefail` and does not `exit $status` on non-timeout failures. As a result:

- Upload/obfuscation runs on incomplete output
- Job and MustGather CR may report success (`Completed`) while the bundle is invalid
- Support cases may receive unusable must-gather archives

## Environment

- **Platform**: OpenShift 4.x [UNKNOWN — specific version not stated in Jira; bug is design-level in operator Job template]
- **Operator Version**: Current `openshift/must-gather-operator` mainline (post MG-157 obfuscation integration) [NEEDS INVESTIGATION: confirm exact release build under test]
- **Cluster Topology**: Any topology with Must-Gather Operator deployed (repro is Job-template behavior, not topology-specific)
- **Architecture**: x86_64 / aarch64 (not architecture-specific)
- **Configuration**: MustGather CR with `uploadTarget` (SFTP) and/or `obfuscate.enabled: true`; optional `storage` PVC for obfuscated output persistence
- **Network**: SFTP upload path requires egress to `sftp.access.redhat.com` (or configured host); PVC-only path does not require SFTP

## Error Evidence

No production log capture was attached to the Jira ticket. Expected observable symptoms from code inspection and reproduction:

**Upload container — pgrep debounce then upload** (from `controllers/mustgather/template.go` `uploadCommand`):

```
waiting for gathers to complete ...
no gather is running (0 / 4)
no gather is running (1 / 4)
...
Running obfuscation on /must-gather ...
Archiving files from /must-gather-upload/cleaned to /must-gather-upload/<prefix>.tar.gz
Uploading '<prefix>.tar.gz' to Red Hat Customer SFTP Server for case <caseid>
```

**Gather container — simulated failure** (repro CR above):

```
simulated gather failure
```

**Gather container — default command exit-code masking** (from `gatherCommand` in `template.go`):

```bash
timeout %v bash -x -c -- '/usr/bin/gather' 2>&1 | tee /must-gather/must-gather.log

status=$?
if [[ $status -eq 124 || $status -eq 137 ]]; then
  echo "Gather timed out."
  exit 0
fi | tee -a /must-gather/must-gather.log
```

Note: non-timeout gather failures are not followed by `exit $status`; pipeline exit status may reflect `tee` rather than `gather`.

**Upload script** (`build/bin/upload`): validates obfuscation exit code but performs no gather-success check before obfuscation or tar/upload.

## Feature Context (from Linked Epic)

### Epic: MG-297 — Integrate must-gather-clean with must-gather-operator

Epic goal: add obfuscation of must-gather bundle data by integrating `must-gather-clean` functionality into the Must-Gather Operator. Acceptance criteria: when enabled, produce obfuscated must-gather archives using the support log gather operator.

Related enhancement: [Bundle Obfuscation (MG-293)](https://github.com/openshift/enhancements/blob/master/enhancements/support-log-gather/must-gather-bundle-obfuscation.md) — three modes: gather+obfuscate+upload, obfuscate-only (via `obfuscate.source`), obfuscate+upload.

### Original Design Intent (ARD)

- **Two-container Job with `pgrep` coordination** (ADR-0002): Use a single Job with gather and conditional upload containers sharing `ShareProcessNamespace: true`. Upload polls for gather completion via `pgrep` (5 consecutive checks, 30s apart) rather than init containers. Rationale: separation of gather image vs upload logic, shared `/must-gather` volume, simpler lifecycle than chained Jobs. Known consequence documented: "`pgrep` polling is fragile — depends on process naming conventions" and "Upload container must handle the case where gather times out (exit 124/137 mapped to exit 0)".

- **Obfuscation before upload** (PR #376 / MG-157): Integrate `must-gather-clean` into the operator. Upload script runs `must-gather-operator obfuscate` on gather output before tar/SFTP upload. PVC reuse logic added for gather+obfuscate+PVC to avoid CSI multi-attach failures.

- **Obfuscation logging** (PR #381 / MG-336): Capture obfuscation stdout/stderr to `obfuscation.log` alongside cleaned output.

- **Direct upload path for `obfuscate.source`** (PR #376): When `obfuscate.source` is set (upload-only, no gather), upload uses `uploadCommandDirect` (`/usr/local/bin/upload` immediately) — gather wait loop is skipped by design.

## Development PR Context

### PRs that implemented the feature

| PR | Title | Author | Merged | Key Changes |
|----|-------|--------|--------|-------------|
| [#376](https://github.com/openshift/must-gather-operator/pull/376) | MG-157: Integrate must-gather-clean in must-gather-operator | (from git merge) | — | Obfuscate API, upload script obfuscation, template.go volume/mount changes, E2E |
| [#381](https://github.com/openshift/must-gather-operator/pull/381) | MG-336: Added obfuscation logs to must-gather | (from git merge) | — | Obfuscation log capture in upload script |
| [#378](https://github.com/openshift/must-gather-operator/pull/378) | MG-331: Promote the must-gather-operator from Tech Preview to GA | (from git merge) | — | GA promotion (context only) |

*Note: Jira development panel returned access denied for MG-297/MG-357. PR table populated from local `git log`.*

### PR Diff Summary

- **`controllers/mustgather/template.go`** (PR #376): Extended `getUploadContainer` for obfuscation env vars and volume mounts; added `uploadCommandDirect` for `obfuscate.source` mode; PVC reuse for obfuscate+storage; `gatherCommand` unchanged regarding exit-code propagation. `uploadCommand` retains `pgrep -a gather` debounce → `/usr/local/bin/upload`.

- **`build/bin/upload`** (PR #376, #381): Added obfuscation block invoking `must-gather-operator obfuscate`; checks obfuscation exit code; no gather-success validation. SFTP upload skipped when obfuscation-only without credentials.

- **`api/v1/mustgather_types.go`** (PR #376): Added `ObfuscateConfig` with CEL validation rules tying obfuscation to uploadTarget, storage, or source.

- **`pkg/mustgatherutil/obfuscate.go`** (PR #376): CLI wrapper for must-gather-clean obfuscation engine.

### Key Code Paths Affected

- `controllers/mustgather/template.go`: `gatherCommand`, `uploadCommand`, `getGatherContainer`, `getUploadContainer` — Job container commands and coordination logic
- `build/bin/upload`: Post-gather obfuscation, compression, and SFTP upload — no gather exit validation
- `controllers/mustgather/mustgather_controller.go`: Job status propagation to MustGather CR conditions
- `api/v1/mustgather_types.go`: `obfuscate`, `uploadTarget`, `gatherSpec.command` — CR fields that trigger upload container presence

## Assumptions

- **A-001**: Severity classified as **Major** based on impact narrative (partial/failed bundles may reach Red Hat SFTP and support cases) — Jira did not provide explicit severity/priority fields (Stage 0 `missing_elements`).
- **A-002**: Reproduction requires a live OpenShift cluster with the Must-Gather Operator installed; PVC-only obfuscation paths can be verified without SFTP credentials (Stage 0 `quality_issues` reproducibility).
- **A-003**: The failing-gather repro CR with `gatherSpec.command` is a valid deterministic trigger; equivalent failure can be simulated via gather image crash or early exit (Stage 0 `quality_issues` specificity).
- **A-004**: Bug affects all OpenShift versions running the current operator Job template — not limited to a specific OCP minor release (Stage 0 `non_blockers` — no version in ticket).
- **A-005**: Representative log lines in Error Evidence are derived from operator source code and expected container output, not from an attached Jira log capture (Stage 0 `non_blockers`).
- **A-006**: PR metadata (author, merge date) sourced from `git log`; full PR descriptions were not fetched due to Jira dev-panel access denial and GitHub API unavailability (Stage 0 `non_blockers`).

---

## Quality Self-Check

- [x] Steps to reproduce are numbered and can be followed by an engineer with no prior context
- [x] Expected and actual behavior are both explicitly stated
- [x] Error evidence includes representative log lines and code excerpts (no raw cluster capture available in ticket)
- [x] Linked Epic is identified and original design intent is documented from ADR-0002 and PR #376
- [x] PR diff summary covers files relevant to the bug area
- [x] At most 2 [NEEDS INVESTIGATION] markers remain (1 used: operator release version)
- [x] Every [UNKNOWN] field has a corresponding Assumption entry (A-004)
- [x] No root cause speculation — only observable facts and aggregated context
