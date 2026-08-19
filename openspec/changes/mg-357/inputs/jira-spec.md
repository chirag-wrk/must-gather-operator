# MG-357 — Jira Ticket

**URL:** https://issues.redhat.com/browse/MG-357  
**Type:** Story  
**Status:** To Do  
**Epic:** [MG-297](https://issues.redhat.com/browse/MG-297) — Integrate must-gather-clean with must-gather-operator

## Summary

Fix Must-gather upload runs after gather process exits without verifying gather succeeded, allowing partial/failed bundles to be obfuscated and uploaded

## Description

When the Must-Gather Operator Job includes an upload container (SFTP upload and/or obfuscation enabled), the upload container coordinates with the gather container only by polling for process absence via `pgrep -a gather`. It does **not** check whether gather completed successfully.

Once `pgrep` reports no running `gather` process (after 5 consecutive checks, 30s apart), upload unconditionally runs `/usr/local/bin/upload`, which may obfuscate and upload whatever is present on the shared volume — including partial or failed gather output.

### Affected component

Must-Gather Operator (`controllers/mustgather/template.go`, `build/bin/upload`)

### Root cause

- Upload wait logic in `uploadCommand` only detects process termination, not success:
  - `pgrep -a gather` → debounce loop → `/usr/local/bin/upload`
- `build/bin/upload` has no gather success validation (no exit-code check, no success marker, no log validation).
- Default gather script (`gatherCommand`) can exit 0 on gather failure (no `pipefail`, `$?` reflects `tee` not `gather`, no `exit $status` on failure), which can cause the Job — and MustGather CR — to reach `Completed` with a bad bundle on SFTP/PVC.

### Impact

- Partial or failed diagnostic bundles may be obfuscated and uploaded to Red Hat SFTP.
- Obfuscated output may be persisted to PVC with `obfuscate.enabled` + `storage`.
- MustGather CR may show `Completed` while the bundle is incomplete or invalid.
- Support cases may receive unusable must-gather archives.

### How reproducible

Always (by design of current upload coordination)

### Steps to reproduce

1. Create a MustGather CR with `uploadTarget` (SFTP) and/or `obfuscate.enabled: true`.
2. Use a gather image/command that fails or exits early (or simulate gather crash).
3. Observe upload container logs: after `pgrep` finds no gather process, upload/obfuscation proceeds.
4. Check Job/MustGather status and uploaded artifact (SFTP or PVC).

### Actual results

Upload/obfuscation runs on incomplete output; Job/CR may still succeed if gather container exits 0.

### Expected results

Upload/obfuscation must run only after gather completes **successfully**. On gather failure, upload must be skipped and the Job/MustGather CR must reflect failure.

### Additional info

- Code: `controllers/mustgather/template.go` (`uploadCommand`, `gatherCommand`)
- ADR: `harness-evals/harness-docs/decisions/adr-0002-two-container-job.md` documents `pgrep`-based coordination
- Related: gather exit-code masking in default `gatherCommand` should be fixed as part of or alongside this work

## Acceptance Criteria

- Upload container does **not** run obfuscation or SFTP upload when the gather container exits non-zero.
- Upload container does **not** run obfuscation or SFTP upload when gather output is incomplete (e.g. gather crash mid-run), even if the gather process is no longer running.
- Default gather command correctly propagates non-zero exit codes on gather failure (`pipefail` and/or explicit `exit $status`); timeout handling (124/137) behavior is documented and tested.
- When gather fails, the Kubernetes Job fails and the MustGather CR status is `Failed` (not `Completed`); no bundle is uploaded to SFTP.
- When gather fails with `obfuscate.enabled` + PVC storage (no SFTP), no cleaned/obfuscated output is written to the PVC.
- When gather succeeds, existing flows still work: gather → obfuscate (if enabled) → upload (if configured).
- Unit tests cover upload wait logic and gather exit-code propagation; e2e or integration test covers gather-failure → no-upload path.
- `obfuscate.source` (upload-only, no gather) behavior is unchanged.
