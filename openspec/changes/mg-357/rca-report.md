# Root Cause Analysis Report
**Bug**: MG-357 — Upload/obfuscation proceeds after gather exits without success verification
**Analysis Date**: 2026-08-19
**Root Cause Identified**: Yes

## 0. Inputs Acknowledged
| Input | Status |
|-------|--------|
| repro-verification-report.md | `openspec/changes/mg-357/repro-verification-report.md` |
| bug-report.md | `openspec/changes/mg-357/bug-report.md` |
| ard-context.md | NOT_PROVIDED (ARD extracted from bug-report.md + ADR-0002) |
| pr-diffs/ | 2 PRs referenced from git log (#376, #381); full diffs NOT_PROVIDED |
| agents.md | `AGENTS.md` (repo root) |

## 1. Failure Path Analysis

### Symptom
Partial or failed must-gather bundles are obfuscated and uploaded to Red Hat SFTP (or written to PVC), while the MustGather CR may report `Completed`. (repro-verification-report.md §3 Failure Signature)

### Failure Trace

1. **Upload runs after gather process exits** — repro evidence: `uploadCommand` ends with unconditional `/usr/local/bin/upload` after `pgrep -a gather` finds no running process
   - Source: `controllers/mustgather/template.go` — `uploadCommand` constant (line 52), assembled in `getUploadContainer()` (line 392–399)
   - Evidence: repro-verification Step 3 — no exit-code, success-marker, or log validation between pgrep debounce and upload invocation

2. **Upload script obfuscates/uploads without gather guard** — traced through `build/bin/upload`
   - Source: `build/bin/upload` lines 68–102 (obfuscation block), 101–135 (tar + SFTP)
   - Evidence: No reference to gather exit status, `.gather_rc`, or `must-gather.log` success validation before obfuscation begins

3. **Gather container may exit 0 on failure** — shell simulation and `gatherCommand` structure
   - Source: `controllers/mustgather/template.go` — `gatherCommand` constant (line 32), applied in `getGatherContainer()` (line 348–356)
   - Evidence: repro-verification shell simulation — `false | tee` → exit 0 without `pipefail`; `gatherCommand` captures `$?` after pipeline but only handles timeout codes 124/137, never `exit $status` for other failures; trailing `| tee -a` further decouples container exit from gather binary status

4. **Root Cause (primary)**: The upload container coordination logic (`uploadCommand`) uses **process absence as a proxy for gather success**, with no verification of gather exit status or output completeness. This is a **missing check** in the two-container Job design.
   - Source: `controllers/mustgather/template.go:52` (`uploadCommand`)
   - Evidence: ADR-0002 explicitly documents pgrep-based polling but does not require success validation; repro-verification confirms unconditional `/usr/local/bin/upload` after debounce

5. **Root Cause (contributing)**: The default `gatherCommand` **does not propagate non-timeout gather failures** to the gather container exit code due to missing `pipefail` and missing `exit $status`, compounding the upload coordination gap.
   - Source: `controllers/mustgather/template.go:32` (`gatherCommand`)
   - Evidence: repro-verification Step 4 shell simulation; bug-report.md Error Evidence

### Evidence Summary
- **Log evidence**: repro-verification code-path evidence — `no gather is running (N / 4)` → `/usr/local/bin/upload`; shell simulation `exit status without pipefail: 0`
- **Code evidence**: `template.go:32,52`; `build/bin/upload:68–102`; `mustgather_controller.go:296–298` (Job `Succeeded` → CR `Completed`)
- **PR evidence**: PR #376 (MG-157) added obfuscation to upload path atop existing pgrep coordination; PR #381 added obfuscation logging — neither added gather-success gate

## 2. ARD Intent vs Actual Behavior

### Original Intent (from PR descriptions)
- **ADR-0002**: Two-container Job — gather collects diagnostics; upload polls via `pgrep` (5×30s) then compresses/uploads. `ShareProcessNamespace` enables cross-container process visibility. Acknowledged trade-off: "pgrep polling is fragile."
- **PR #376 (MG-157)**: Integrate `must-gather-clean` — upload script runs obfuscation on gather output before tar/SFTP upload. Upload container added when obfuscation or upload target configured.
- **Epic MG-297**: Produce obfuscated must-gather archives when enabled — implies valid gather output as input.

### Actual Behavior
- Upload waits only for `pgrep -a gather` to return empty, then runs obfuscation/upload regardless of gather outcome.
- Default gather command can exit 0 when gather binary fails (pipeline exit-code masking).
- Controller marks MustGather `Completed` when Job `Status.Succeeded > 0` (`mustgather_controller.go:296–298`) — if gather exits 0 and upload succeeds, CR reports success with a bad bundle.

### Divergence Point
**`controllers/mustgather/template.go:52` (`uploadCommand`)** — ADR-0002 and PR #376 implemented coordination as "wait for gather process to stop" but never added "verify gather succeeded." When obfuscation was added (PR #376), failed/partial gather output became actively harmful (cleaned and uploaded to SFTP), but the coordination gap predates obfuscation and affects SFTP-only flows too.

## 3. PR Diff Comparison

### Original PR Changes
- **PR #376**: Modified `template.go` (obfuscation env/volumes, `uploadCommandDirect` for source mode), `build/bin/upload` (obfuscation block), API types, E2E tests. Retained existing `uploadCommand` pgrep loop.
- **PR #381**: Added obfuscation log capture in `build/bin/upload`.

### Current Code State
- `uploadCommand` pgrep debounce unchanged from pre-obfuscation design.
- `gatherCommand` exit-code handling unchanged — timeout (124/137) mapped to exit 0 per ADR-0002 consequence note.
- No unit test asserts upload waits for gather **success** (only source-mode direct upload tested at `template_test.go:933–937`).

### Change That Introduced the Bug
The defect is an **omission in the original two-container design** (pre-MG-157), not a regression from PR #376. PR #376 **amplified impact** by obfuscating and uploading bad bundles, but the missing success gate exists in `uploadCommand` since pgrep coordination was introduced. The `gatherCommand` exit-code masking is a separate omission in the same template file.

## 4. Root Cause Statement

**Root Cause**: The upload container's `uploadCommand` shell script treats cessation of the `gather` process (detected via `pgrep -a gather`) as completion, without checking gather exit status, a success marker, or output validity — causing obfuscation and SFTP upload to run on failed or partial gather output. The default `gatherCommand` compounds this by not propagating non-timeout gather failures to the container exit code.

**Type**: Missing check (primary); Missing error handling (contributing)

**Introduced by**: Original two-container Job design (ADR-0002 era); impact amplified by PR #376 (obfuscation integration)

**Why this is root cause and not a symptom**: "Bad bundle uploaded to SFTP" is the symptom. The defect is the **absence of a gather-success gate** in `uploadCommand` (and exit-code propagation in `gatherCommand`) — fixing these prevents upload/obfuscation on failed gather and allows the Job/CR to reflect failure correctly.

## 5. Affected Components

| Component | File/Package | Impact | Agent (from AGENTS.md) |
|-----------|-------------|--------|------------------------|
| Job Template | `controllers/mustgather/template.go` | `uploadCommand` and `gatherCommand` define defective coordination and exit-code behavior | Job Template (`template.go`) |
| Upload Script | `build/bin/upload` | No pre-obfuscation gather validation | Upload Script (`build/bin/upload`) |
| Controller | `controllers/mustgather/mustgather_controller.go` | Propagates Job success to CR `Completed` when upload succeeds despite bad gather | Controller (`mustgather_controller.go`) |
| Unit Tests | `controllers/mustgather/template_test.go` | No coverage for gather-failure → no-upload path | Testing (`MGO_TESTING.md`) |

## 6. Fix Recommendation

### Fix Area
- **Files to modify**:
  - `controllers/mustgather/template.go` — `uploadCommand`, `gatherCommand`
  - `build/bin/upload` — optional secondary guard (prefer fix in template coordination)
  - `controllers/mustgather/template_test.go` — new unit tests
  - `test/e2e/must_gather_operator_test.go` — gather-failure integration test (if feasible)

- **Changes needed**:
  1. **`uploadCommand`**: After pgrep debounce, verify gather success before invoking `/usr/local/bin/upload`. Options: read gather container exit code via shared process namespace (wait on gather PID), check a success marker file written by gather on success, or read exit status from a shared file. Upload must skip obfuscation/SFTP and exit non-zero on gather failure.
  2. **`gatherCommand`**: Add `set -o pipefail` and `exit $status` for non-timeout failures; document and test timeout handling (124/137) per Jira AC.
  3. **`obfuscate.source` path**: Preserve `uploadCommandDirect` behavior — no gather wait when gather is skipped.
  4. **Controller**: Ensure Job fails when upload skips due to gather failure (upload container must exit non-zero).

- **Minimal blast radius**: Changes confined to Job shell commands and tests; no API/CRD changes required. `obfuscate.source` direct-upload path must be excluded from new gather-success checks.

### Regression Prevention
- **Unit test needed**: Assert generated upload command includes gather-success verification; assert `gatherCommand` propagates non-zero exit from failed gather (mock/script fragment test). Assert `uploadCommandDirect` unchanged for source mode.
- **E2E test needed**: Apply failing-gather MustGather CR; verify upload container does not obfuscate/upload; Job and CR reach `Failed`.
- **Existing test gap**: `Test_getUploadContainer` validates env vars and mounts but not upload wait semantics; no test for gather exit-code propagation in default command.

## 7. Assessment Confidence
- **Root cause confidence**: High
- **Evidence quality**: Strong for code-path root cause (deterministic, confirmed in repro-verification); moderate for end-to-end Job/CR behavior (live cluster not tested)
- **Unresolved questions**:
  - Preferred coordination mechanism: exit-code file vs success marker vs wait on gather PID in shared process namespace?
  - Should gather crash mid-run (process gone, partial output) be detected via output validation in addition to exit code?
- **Alternative hypotheses considered**:
  - *Controller incorrectly marks success*: Ruled as downstream effect — controller correctly reflects Job status; Job succeeds because gather exits 0 and upload completes.
  - *Obfuscation script bug*: Ruled out — obfuscation works as designed; it lacks input validation, which is the upload coordination gap.
  - *pgrep naming mismatch*: Possible edge case for custom gather images, but not the primary defect — even when pgrep works, success is not verified.
