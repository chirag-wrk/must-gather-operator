# Repro Verification Report
**Bug**: MG-357 — Upload/obfuscation proceeds after gather exits without success verification
**Verified on**: 2026-08-19
**Bug Confirmed**: Yes

## 0. Inputs & Environment
- Bug report: `openspec/changes/mg-357/bug-report.md`
- Repo: `openshift/must-gather-operator` (working-folder mode), branch: `master`, commit: `9696949`
- Environment: No live OpenShift cluster (kubeconfig API unreachable — DNS resolution failure)
- Log source: repo-local code-path verification, shell simulation
- Execution mode: Cursor agent chat / repo-local
- agents.md: `AGENTS.md` (repo root; `openspec/inputs/agents.md` is stub)

## 1. Reproduction Steps Executed

### Step 1: Deploy operator and prerequisites
- **Action taken**: Verified cluster access via `oc whoami` / `oc version`. Attempted connection to configured API server.
- **Observed result**: `Unable to connect to the server: dial tcp: lookup api.tt3gq-8jbmz-2mk.yd65.p3.openshiftapps.com: Temporary failure in name resolution`. Live cluster deployment skipped.
- **Expected result**: Operator deployed on OpenShift cluster with SFTP Secret and ServiceAccount.
- **Status**: SKIP (no live cluster)
- **Evidence**: `oc whoami` output (2026-08-19)

### Step 2: Apply failing-gather MustGather CR
- **Action taken**: Inspected approved repro CR in `bug-report.md` (`gatherSpec.command` exits 1 with `obfuscate.enabled` + `uploadTarget`). Verified CRD supports `gatherSpec.command` via `api/v1/mustgather_types.go` and example patterns in `examples/mustgather_obfuscate_gather_upload.yaml`.
- **Observed result**: CR shape is valid per API validation rules. Job template would add upload container when obfuscate is enabled (`shouldAddUploadContainer` returns true for `obfuscate.enabled: true`).
- **Expected result**: Operator creates Job with gather + upload containers.
- **Status**: PASS (repo-local API/template verification)
- **Evidence**: `controllers/mustgather/template_test.go` — `shouldAddUploadContainer` test; `api/v1/mustgather_types.go`

### Step 3: Observe upload container logs after gather failure
- **Action taken**: Inspected `uploadCommand` constant in `controllers/mustgather/template.go` and traced upload container command assembly in `getUploadContainer()`.
- **Observed result**: Upload wait loop uses only `pgrep -a gather` to detect process absence. After five consecutive 30-second intervals with no matching process, command unconditionally runs `/usr/local/bin/upload`. No check for gather exit code, success marker, or log validation.
- **Expected result** (per bug report): Upload should **not** run when gather fails.
- **Status**: FAIL (confirms bug — upload proceeds regardless of gather outcome)
- **Evidence**:

```go
// controllers/mustgather/template.go:52
uploadCommand = "count=0\nuntil [ $count -gt 4 ]\ndo\n  while `pgrep -a gather > /dev/null`\n  do\n    echo \"waiting for gathers to complete ...\"\n    sleep 120\n    count=0\n  done\n  echo \"no gather is running ($count / 4)\"\n  ((count++))\n  sleep 30\ndone\n/usr/local/bin/upload"
```

### Step 4: Verify Job/MustGather status and artifacts
- **Action taken**: Inspected default `gatherCommand` in `template.go` and simulated pipeline exit-code behavior with shell. Reviewed `build/bin/upload` for pre-upload gather validation.
- **Observed result**:
  1. **Gather exit-code masking**: Shell simulation `false 2>&1 | tee /tmp/mgo-test.log; status=$?` → `exit status without pipefail: 0`. Default `gatherCommand` uses `tee` pipeline without `set -o pipefail` and does not `exit $status` on non-timeout failures — gather container can exit 0 when gather binary fails.
  2. **Upload script**: `build/bin/upload` checks obfuscation exit code but has no gather-success validation before obfuscation or SFTP upload.
- **Expected result**: Job/CR should fail; no upload/obfuscation on gather failure.
- **Status**: FAIL (confirms bug — failure can be masked and upload has no gather guard)
- **Evidence**:

```bash
# Shell simulation (2026-08-19)
$ bash -c 'false 2>&1 | tee /tmp/mgo-test.log; status=$?; echo "exit status without pipefail: $status"'
exit status without pipefail: 0

$ bash -c 'set -o pipefail; false 2>&1 | tee /tmp/mgo-test2.log; status=$?; echo "exit status with pipefail: $status"'
exit status with pipefail: 1
```

```go
// controllers/mustgather/template.go:32 (gatherCommand excerpt)
gatherCommand = "timeout %v bash -x -c -- '/usr/bin/%v' 2>&1 | tee /must-gather/must-gather.log\n\nstatus=$?\nif [[ $status -eq 124 || $status -eq 137 ]]; then\n  echo \"Gather timed out.\"\n  exit 0\nfi | tee -a /must-gather/must-gather.log"
```

## 2. Logs Captured

### Operator Logs
```
N/A — live cluster unavailable. Repo-local verification used source inspection and shell simulation instead of operator pod logs.
```

### Kubernetes Events
```
N/A — live cluster unavailable.
```

### Error Traces / Code-Path Evidence
```
# uploadCommand terminal action (no success gate):
/usr/local/bin/upload

# upload script obfuscation entry (no gather check precedes this):
if [ "${obfuscate}" = "true" ]; then
  echo "Running obfuscation on $must_gather_output ..."
  ...
fi

# ADR-0002 acknowledges pgrep fragility:
# "pgrep polling is fragile — depends on process naming conventions"
# "Upload container must handle the case where gather times out (exit 124/137 mapped to exit 0)"
```

## 3. Failure Signature
Upload container treats gather **process absence** as gather **completion**, then unconditionally invokes obfuscation and/or SFTP upload. Gather container may simultaneously report success (exit 0) due to pipeline exit-code masking in the default gather command.

- **Error type**: Wrong state / premature success
- **Error location**: Job upload container (`uploadCommand` in `controllers/mustgather/template.go`); gather container default command (`gatherCommand`); `build/bin/upload` (no gather guard)
- **Trigger condition**: MustGather CR with `uploadTarget` and/or `obfuscate.enabled: true`; gather process exits (success, failure, or crash) such that `pgrep -a gather` finds no running process
- **Frequency**: Every time (by design of current upload coordination — per Jira MG-357)

## 4. Environment Details
- Platform: OpenShift 4.x (not live-tested; bug is operator Job-template behavior)
- Operator version: `master` @ `9696949` (working-folder checkout)
- Configuration: MustGather with `obfuscate.enabled: true` and/or `uploadTarget.type: SFTP`
- Prerequisites: Upload container added when `shouldAddUploadContainer()` is true (upload target or obfuscation enabled)
- Differences from reported environment: Live cluster e2e not executed; confirmation via repo-local code-path analysis and shell simulation

## 5. Reproduction Confidence
- **Reproducibility**: Always (design-level — confirmed in source)
- **Confidence level**: High (code-path evidence is deterministic; Jira states "Always reproducible")
- **Notes**: End-to-end Job/Pod log capture on a live cluster would strengthen evidence but is not required to confirm the coordination gap exists in the template.

## 6. Assessment Limitations
- Live OpenShift cluster unavailable (`oc` DNS resolution failure) — Steps 1 and live log capture from Steps 3–4 not executed on cluster.
- `go` toolchain not available in execution environment — unit tests (`go test ./controllers/mustgather/...`) not run; template behavior verified via source inspection and existing test file references.
- SFTP upload and PVC persistence not observed end-to-end; inferred from `build/bin/upload` flow.
- `obfuscate.source` direct-upload path (`uploadCommandDirect`) not re-tested here; bug report states it must remain unchanged.
