# Bug Fix Plan
**Bug**: MG-357 — Upload/obfuscation proceeds after gather exits without success verification
**Root Cause**: `uploadCommand` treats gather process absence as success (no exit-code gate); `gatherCommand` masks non-timeout failures

## 0. Inputs Acknowledged

| Input | Status |
|-------|--------|
| rca-report.md | PROVIDED — `openspec/changes/mg-357/rca-report.md` |
| bug-report.md | PROVIDED — `openspec/changes/mg-357/bug-report.md` |
| repro-verification-report.md | PROVIDED — `openspec/changes/mg-357/repro-verification-report.md` |
| constitution.md | PROVIDED — `openspec/changes/mg-357/inputs/constitution.md` (generated) |
| agents.md | `AGENTS.md` (repo root) |
| AgentRoutingMode | PROVIDED (component-based per AGENTS.md) |

## 1. Root Cause Summary

The upload container's `uploadCommand` waits for `pgrep -a gather` to return empty, then unconditionally runs `/usr/local/bin/upload`. It never verifies gather exit status or output validity. The default `gatherCommand` compounds this: without `pipefail` and without `exit $status` for non-timeout failures, the gather container can exit 0 when the gather binary fails.

**RCA confidence:** High (rca-report.md §7)

### Affected Code Paths

| File | Function/Constant | Role |
|------|-------------------|------|
| `controllers/mustgather/template.go` | `uploadCommand` (L52), `getUploadContainer()` | Upload wait + invoke upload |
| `controllers/mustgather/template.go` | `gatherCommand` (L32), `getGatherContainer()` | Gather execution + exit propagation |
| `build/bin/upload` | main script | Obfuscation + SFTP (no gather guard) |
| `controllers/mustgather/template_test.go` | unit tests | Missing gather-failure coverage |
| `test/e2e/must_gather_operator_test.go` | e2e | Missing gather-failure → no-upload test |

## 2. Fix Approach

### Strategy

**Two coordinated shell-command fixes in `template.go`:**

1. **`gatherCommand`**: Enable `set -o pipefail`. After gather runs, capture exit status. For timeout (124/137), keep existing behavior (exit 0 with "Gather timed out" message per ADR-0002). For all other non-zero statuses, `exit $status`. Write exit code to a shared marker file on the output volume (e.g. `/must-gather/.gather_exit`) so the upload container can read it across containers via the shared volume.

2. **`uploadCommand`**: After the existing pgrep debounce loop, read `/must-gather/.gather_exit`. If missing or non-zero, log failure and `exit 1` without invoking `/usr/local/bin/upload`. Only call `/usr/local/bin/upload` when gather exit is 0.

3. **Preserve `uploadCommandDirect`**: No gather wait or exit check when `obfuscate.source` is set (gather skipped by design).

4. **Upload container exit non-zero on gather failure** → Kubernetes Job fails → controller sets MustGather `Failed` (existing `mustgather_controller.go` logic).

### Minimal Blast Radius Justification

All changes are confined to Job shell commands in `template.go` and tests. No API/CRD/controller logic changes required — the controller already propagates Job failure correctly. `build/bin/upload` unchanged unless SME prefers a secondary guard (deferred — primary fix in template coordination).

### Alternative Approaches Considered

| Alternative | Why rejected |
|-------------|--------------|
| Wait on gather PID via `ShareProcessNamespace` | Fragile across container boundaries; harder to test; larger script complexity |
| Output validation (check bundle completeness) | Broader scope; harder to define "complete"; exit-code gate addresses Jira AC |
| Fix only `gatherCommand` exit propagation | Insufficient — upload still runs when gather crashes mid-run with partial output if process exits |
| Secondary guard in `build/bin/upload` only | Upload script lacks gather context; template is the coordination owner per ADR-0002 |

## 3. Files to Change

| File | Change Type | Purpose | Confidence |
|------|------------|---------|------------|
| `controllers/mustgather/template.go` | Modify | Fix `gatherCommand` exit propagation; add gather-exit check to `uploadCommand` | High |
| `controllers/mustgather/template_test.go` | Modify | Unit tests for command strings and exit-check logic | High |
| `test/e2e/must_gather_operator_test.go` | Modify | E2E gather-failure → no-upload path | Medium |
| `harness-evals/harness-docs/decisions/adr-0002-two-container-job.md` | Modify | Document gather-success gate (optional, if AC requires doc) | Low |

## 4. Regression Test Strategy

### Unit Tests

- Assert `uploadCommand` (non-source mode) contains gather exit-code check and does not invoke upload on non-zero exit.
- Assert `gatherCommand` contains `pipefail` and `exit $status` for non-timeout failures.
- Assert timeout codes 124/137 still exit 0 (documented behavior).
- Assert `uploadCommandDirect` unchanged for `obfuscate.source` mode (`template_test.go` existing pattern).

### Regression E2E Test (if applicable)

Add e2e case: MustGather with `gatherSpec.command` exiting 1 + `obfuscate.enabled` → Job fails, upload logs show no obfuscation, CR `Failed`.

### Existing Test Impact

`Test_getUploadContainer` may need assertion updates if upload command string changes. No behavior change for source-mode direct upload tests.

### Verification

After fix, repro CR from bug-report.md should yield: gather fails → upload exits 1 → Job `Failed` → no SFTP upload / no PVC obfuscated output.

## 5. Rollback Plan

`git revert <commit>` — shell command changes only, no schema migration or data changes.

## 6. Risk Assessment

| Risk | Impact | Mitigation |
|------|--------|------------|
| Custom gather images don't write `.gather_exit` | Upload may fail incorrectly | Only default `gatherCommand` writes marker; custom `gatherSpec.command` users responsible for coordination |
| Timeout still exits 0 per ADR | Upload runs on timed-out gather | Documented existing behavior; Jira AC allows timeout 124/137 handling to remain |
| Race: upload reads marker before gather writes | False failure | pgrep debounce ensures gather process gone before read |
| Breaking `obfuscate.source` | Regression in source-only mode | Explicit test that `uploadCommandDirect` path unchanged |

## 7. Verification Matrix

| Verification | Command | Traces to |
|-------------|---------|-----------|
| Build passes | `make go-build` | constitution.md |
| Unit tests pass | `make go-test` | root cause fix |
| Lint passes | `make lint` | constitution.md |
| Template regression tests | `go test ./controllers/mustgather/ -run 'Test_getUploadContainer|Test_gatherCommand' -v` | rca-report.md |
| E2E (if cluster available) | `make test-e2e` | bug-report.md repro CR |

## 8. Open Questions / SME Decisions

1. **Coordination mechanism**: Exit-code marker file (`.gather_exit` on shared volume) vs waiting on gather PID?
   - **SME**: MGO maintainers
   - **Default assumption if unanswered**: Shared volume exit-code marker file (simplest, testable, works with ShareProcessNamespace)

2. **Partial output detection**: Should upload also validate bundle completeness beyond exit code (e.g. crash mid-run with exit 0)?
   - **SME**: MGO maintainers / support
   - **Default assumption if unanswered**: Exit-code gate only for MG-357 scope; partial-output validation out of scope unless Jira AC requires it

3. **Timeout behavior**: Should gather timeout (124/137) still allow upload per current ADR-0002?
   - **SME**: MGO maintainers
   - **Default assumption if unanswered**: Preserve current timeout → exit 0 behavior; document in tests

4. **Secondary guard in `build/bin/upload`**: Add gather check in upload script in addition to template?
   - **SME**: Implementer
   - **Default assumption if unanswered**: Template-only fix (minimal blast radius)
