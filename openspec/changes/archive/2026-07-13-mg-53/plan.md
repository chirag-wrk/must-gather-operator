# Technical Implementation Plan

**Feature:** MG-53 — Extensible Must-Gather Upload Targets (SFTP hostname + optional upload)

## 0. Inputs acknowledged

| Input | Status |
|-------|--------|
| Spec source | MG-53 — `specs.md` (approved) |
| Repo assessment pin | `https://github.com/openshift/must-gather-operator`, branch `master`, commit `e766ff0` (tooling_status: OK) |
| `agents.md` | PROVIDED via `openspec/inputs/agents.md` — provisional agent taxonomy used (no formal agent IDs in repo) |
| `spec_validator_results.json` | PROVIDED — `validation.json` (PASS 84%) |
| `constitution.md` | PROVIDED — `openspec/changes/mg-53/inputs/constitution.md` (generated from repo evidence) |
| AgentRoutingMode | PROVISIONAL |
| Implementation mode | Working-folder (`use_working_folder_as_repo: true`) |

## 1. Architectural strategy

MG-53 introduces a discriminated `uploadTarget` configuration on the `MustGather` CR, making SFTP upload optional and allowing a configurable SFTP hostname (defaulting to production Red Hat support). The change spans four layers: CRD/API validation (CEL union), controller Job template (conditional upload container + env wiring), upload shell script (hostname from env), and test/example migration for the breaking API change.

**Repo-grounded reality check:** Per `repo-assessment.md` §0, this is **GREENFIELD on `master` at `e766ff0`**. The branch has flat required `caseID`/`caseManagementAccountSecretRef`, unconditionally schedules upload, and hardcodes `FTP_HOST=sftp.access.redhat.com` in `build/bin/upload:19`. The `uploadTarget` union described in agents.md and the EP is **not** present in committed `mustgather_types.go`. Stale uncommitted `UploadTarget` deepcopy in the working tree must be reconciled via regeneration. Phases therefore implement new API + controller behavior rather than hardening existing uploadTarget code.

Integration approach:
1. Define union API with CEL guards (admission-time rejection of invalid combinations — FR-004).
2. Regenerate CRD/bundle (constitution Principle II).
3. Refactor `getJobTemplate` to append upload container only when `spec.uploadTarget.type == SFTP` (FR-006, FR-001).
4. Pass SFTP hostname through controller env → upload script (FR-002, User Story 1).
5. Migrate examples/tests; document breaking change (FR-008, SC-006).

## 2. Persistence & state

- **Kubernetes objects (source of truth):** `MustGather` CR spec holds upload destination configuration. Job/Pod/Secret copies are derived and reconciled by controller.
- **Operand config/state:** Upload behavior driven by CR `uploadTarget.sftp` fields mapped to upload container env vars (`caseid`, `username`, `password`, `internal_user`, proxy vars, new `sftp_host`). Gather image from `DEFAULT_MUST_GATHER_IMAGE`; upload image from `OPERATOR_IMAGE`.
- **Volume state:** Current branch uses `emptyDir` volumes (`must-gather-output`, `must-gather-upload`) — one Job per CR, no cross-run persistence. **PVC retention is out of scope for MG-53** (spec A-002/A-003; repo-assessment §11.1).
- **Future PVC isolation (deferred):** If PVC support is added later, the plan requires **per-invocation isolation** — use dynamic `subPathExpr` (e.g., pod name or CR UID), **not static subPath** (insufficient for concurrent runs). Concurrent write safety must use unique mount paths per Job; retention/cleanup must delete or archive subpaths on Job completion. MG-53 emptyDir model avoids cross-run overwrite because each Job gets ephemeral storage.

## 3. Interfaces & contracts (operator-native)

### 3.1 Kubernetes APIs (CRDs/CRs)

- **CRD:** `MustGather` (`operator.openshift.io/v1alpha1`, namespaced).
- **New field:** `spec.uploadTarget` — discriminated union with `type: SFTP` and nested `sftp` config (`caseID`, `caseManagementAccountSecretRef`, optional `host`, optional `internalUser`).
- **Removed fields:** top-level `caseID`, `caseManagementAccountSecretRef`, `internalUser` (breaking — FR-008).
- **Validation:** CEL XValidation on `UploadTarget` — SFTP type requires `sftp` block; non-SFTP types forbidden until supported (FR-010). Use `has()` before accessing omitempty union members.
- **Defaults:** `host` defaults to `sftp.access.redhat.com` via kubebuilder default marker (FR-002, A-007).
- **Immutability:** Spec remains immutable after creation (existing CEL/immutability pattern unchanged — A-009).

### 3.2 Controller/runtime interfaces (internal)

- **`getJobTemplate`:** Accept optional upload; single-container Job when `uploadTarget` nil (FR-006).
- **`getUploadContainer`:** Extended signature to accept SFTP hostname; inject `SFTP_HOST` (or equivalent) env var consumed by `build/bin/upload`.
- **Reconcile gating:** Skip secret copy and upload env wiring when upload disabled.
- **Distinct status condition types** (do not use single generic `ReconcileError` for all failures):
  - `UploadConfigurationInvalid` — CR uploadTarget shape invalid at runtime pre-check
  - `UploadCredentialsInvalid` — secret missing or malformed keys
  - `UploadOperatorConfigInvalid` — required operator env vars empty (`OPERATOR_IMAGE`, `DEFAULT_MUST_GATHER_IMAGE`)
  - `UploadJobFailed` — Job upload container failed (surfaced from Job status)
- **Metrics:** Retain `MetricMustGatherTotal` / `MetricMustGatherErrors`; increment on upload failures (FR-009).

### 3.3 Webhooks / admission (if applicable)

N/A — validation via CRD CEL/OpenAPI only; no separate admission webhook in repo.

### 3.4 RBAC / security boundaries (if applicable)

- Existing cluster-scoped RBAC for Jobs, Pods, Secrets sufficient for MG-53.
- Secret copy pattern (CR namespace → operator namespace) applies only when upload enabled.
- SFTP credentials remain in referenced Secret; hostname is non-secret configuration.

### 3.5 Packaging / OLM (if applicable)

- Regenerate `deploy/crds/operator.openshift.io_mustgathers.yaml` and `bundle/manifests/tech-preview/operator.openshift.io_mustgathers.yaml`.
- Operator image rebuild required (upload script change embedded in image).
- No CSV feature-gate changes anticipated.

## 4. Dependencies & sequencing graph

**Critical path:**
1. API types + kubebuilder markers → 2. `make generate && make manifests` → 3. Upload script env contract → 4. Controller/template refactor → 5. Unit tests → 6. Examples/E2E fixture migration → 7. `make validate`

**Parallelizable after Phase 2:**
- Example YAML updates (Phase 6) can draft in parallel with controller work once API shape is frozen.
- Upload script change (Phase 3) can proceed in parallel with controller env wiring once env var name is agreed.

**Blockers:**
- Breaking API must be finalized before example/test migration.
- Upload script env name must match controller template before integration tests.

## 5. Implementation phases (logical sequence; NOT tasks)

### Phase 1: API schema — uploadTarget union

- **Goal:** Define `UploadTarget`, `SFTPUploadTargetConfig`, CEL validation; remove legacy top-level upload fields (FR-001, FR-004, FR-007, FR-008).
- **Dependencies:** None.
- **Target files:** `api/v1alpha1/mustgather_types.go`
- **Required capabilities:** API (provisional)
- **Verification hooks:** CEL rule review; `make generate` succeeds

### Phase 2: Code generation and CRD refresh

- **Goal:** Regenerate deepcopy, OpenAPI, CRD YAML, bundle CRD (constitution Principle II).
- **Dependencies:** Phase 1.
- **Target files:** `api/v1alpha1/zz_generated.deepcopy.go`, `deploy/crds/operator.openshift.io_mustgathers.yaml`, `bundle/manifests/tech-preview/operator.openshift.io_mustgathers.yaml`
- **Required capabilities:** API, ManifestsBindata (provisional)
- **Verification hooks:** `make generate && make manifests && make validate`

### Phase 3: Upload script — configurable SFTP hostname

- **Goal:** Replace hardcoded `FTP_HOST` with env-driven hostname defaulting to `sftp.access.redhat.com` (FR-002, User Story 1).
- **Dependencies:** Phase 1 (env var name agreed).
- **Target files:** `build/bin/upload`
- **Required capabilities:** OperatorController (provisional)
- **Verification hooks:** Manual script review; container rebuild note for Phase 7

### Phase 4: Controller and Job template — conditional upload + env wiring

- **Goal:** Add upload container only when `uploadTarget` set; wire caseID, credentials, internalUser, hostname, proxy; skip secret copy when upload disabled (FR-003, FR-005, FR-006, FR-009).
- **Dependencies:** Phases 1–3.
- **Target files:** `controllers/mustgather/template.go`, `controllers/mustgather/mustgather_controller.go`
- **Required capabilities:** OperatorController (provisional)
- **Verification hooks:** `go test ./controllers/mustgather/... -count=1`
- **Multi-container mount consistency:** The **gather container** and **upload container** share the `must-gather-output` volume at `/must-gather`. Any volume mount path or volume name change MUST be applied to **both containers** in `initializeJobTemplate` / `getGatherContainer` / `getUploadContainer`. Edge case: user-provided hostname must not alter mount paths — hostname is env-only, not a volume subPath input. Verification hook: unit test asserts both containers mount `outputVolumeName` at `volumeMountPath`.

### Phase 5: Unit tests — API wiring and Job shapes

- **Goal:** Cover optional upload (single vs two containers), custom hostname env, default hostname, internalUser passthrough (User Stories 1, 3, 4; SC-001–SC-004).
- **Dependencies:** Phase 4.
- **Target files:** `controllers/mustgather/template_test.go`, `controllers/mustgather/mustgather_controller_test.go`
- **Required capabilities:** Testing (provisional)
- **Verification hooks:** `go test ./controllers/mustgather/... -count=1 -v`

### Phase 6: Examples and E2E fixture migration

- **Goal:** Migrate sample CRs to `uploadTarget` structure; add staging hostname example; update osde2e CR construction (FR-008, SC-006).
- **Dependencies:** Phase 2 (CRD shape stable).
- **Target files:** `examples/mustgather_*.yaml`, `test/must-gather.yaml`, `test/e2e/must_gather_operator_tests.go`
- **Required capabilities:** ManifestsBindata, Testing (provisional)
- **Verification hooks:** `make olm-deploy-yaml-validate`; osde2e in CI (network-dependent)

### Phase 7: Operator env validation and status conditions

- **Goal:** Fail-fast when required environment variables are empty; set distinct condition types for configuration vs credential vs job failures (§3.2 condition types).
- **Dependencies:** Phase 4.
- **Target files:** `controllers/mustgather/mustgather_controller.go` (and `constant.go` if validation type constants added — UNVERIFIED: file may need creation per agents.md; discovery step: check for existing validation helpers on branch)
- **Required capabilities:** OperatorController (provisional)
- **Verification hooks:** Unit tests for empty `OPERATOR_IMAGE` / `DEFAULT_MUST_GATHER_IMAGE`; condition type assertions in controller tests

## 6. Verification matrix (maps to spec acceptance)

| Category | Coverage | Files / Suites | Spec mapping |
|----------|----------|----------------|--------------|
| Unit | UploadTarget Job template shapes; hostname env; optional upload; mount consistency both containers | `controllers/mustgather/template_test.go`, `mustgather_controller_test.go` | SC-001–SC-004, FR-002, FR-006 |
| Unit | Environment variable default validation — fail-fast when `DEFAULT_MUST_GATHER_IMAGE` or `OPERATOR_IMAGE` empty | `mustgather_controller_test.go`, `template_test.go` | FR-009, constitution env contract |
| Unit | Distinct condition types for upload configuration vs credential vs operator config failures | `mustgather_controller_test.go` | FR-009, §3.2 |
| Integration | CRD CEL rejects invalid uploadTarget combinations | CRD apply + kubectl create dry-run (manual) | SC-003, FR-004 |
| E2E | MustGather with uploadTarget SFTP + staging host (network permitting) | `test/e2e/must_gather_operator_tests.go` | SC-001, User Story 1 |
| E2E | MustGather without uploadTarget — gather only | `test/e2e/` | SC-004, User Story 3 |
| Manual / Cluster | SFTP upload to `sftp.access.stage.redhat.com` | `oc apply` staging example; verify bundle at destination | SC-001, SC-005 |
| Manual / Cluster | Breaking change — old flat-field CR rejected by API server | Apply legacy YAML; expect validation error | SC-006, FR-008 |
| N/A | PVC per-invocation isolation | Not tested — PVC out of MG-53 scope; emptyDir per Job | A-002 |

**Preflight commands (from repo-assessment §12):**
```
make validate
make lint
make go-test
go test ./controllers/mustgather/... -count=1 -v
```

## 7. Risks, migrations, and operational follow-ups

- **Upgrade/migration:** Breaking API — all in-cluster MustGather CRs must migrate to `uploadTarget` before/at upgrade (A-005). Provide migration YAML in examples. CRD validation rejects legacy shape (SC-006).
- **Compatibility (OpenShift/MicroShift/Hypershift):** No topology-specific upload behavior (A-004). MicroShift out of scope (A-003).
- **Hardcoded hostname regression:** Until Phase 3 ships in operator image, staging uploads hit production — mitigate with image rebuild in release pipeline.
- **Multi-container mount drift:** Changing volume config in one container only breaks upload — Phase 4 verification hook mandatory.
- **Stale deepcopy in working tree:** Regenerate in Phase 2 to avoid type/deepcopy mismatch.
- **Concurrent PVC (future):** If PVC added post-MG-53 without per-invocation isolation (`subPathExpr`), data overwrite risk across runs — document in release notes; not introduced by this plan.
- **SFTP E2E in disconnected CI:** Tag network tests `[Skipped:Disconnected]` per agents.md PR expectations.

## 8. Open questions / SME decisions

| Question | Owner | Default if unresolved |
|----------|-------|----------------------|
| Exact env var name for SFTP hostname in upload script (`SFTP_HOST` vs `FTP_HOST` override)? | OperatorController SME | Use `SFTP_HOST` in script with fallback to `sftp.access.redhat.com`; controller sets env in `getUploadContainer` |
| Add runtime validation helpers (`validation.go`, `constant.go`) per agents.md or inline in controller? | OperatorController SME | Inline validation in controller/template first; extract to `validation.go` only if complexity warrants |
| Should operator block reconcile when `DEFAULT_MUST_GATHER_IMAGE` empty (currently silent empty image)? | OperatorController SME | Fail-fast with `UploadOperatorConfigInvalid` condition per constitution env contract |
| Publish openshift-docs/user-facing migration guide in this change? | Docs/release SME | Out of implementation scope; note in implementation-report only |

None beyond the above — all core MG-53 decisions resolved with stated defaults.
