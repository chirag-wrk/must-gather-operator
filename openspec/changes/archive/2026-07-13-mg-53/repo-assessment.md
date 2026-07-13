# Repository Assessment Report

**Feature:** MG-53 — Extensible Must-Gather Upload Targets (SFTP hostname + `uploadTarget` API)

## 0. Inputs & Tooling

| Field | Value |
|-------|-------|
| **repo** | `https://github.com/openshift/must-gather-operator` (working copy: `git@github.com:chirag-wrk/must-gather-operator.git`) |
| **branch** | `master` |
| **commit** | `e766ff0b5ff1dba75d06639b99853539a879e049` |
| **tooling_status** | OK |
| **spec status** | Approved (`specs.md`, validation PASS 84%) |
| **assessment mode** | Working-folder mode (`use_working_folder_as_repo: true`) |

**Feature implementation status on pinned branch:** **GREENFIELD** — `uploadTarget` discriminated union, optional upload, and configurable SFTP hostname are **NOT implemented** on `master` at commit `e766ff0`. Current code uses flat required `caseID` / `caseManagementAccountSecretRef`, always schedules an upload container, and hardcodes `FTP_HOST=sftp.access.redhat.com` in `build/bin/upload`. Local working tree has stale `UploadTarget` deepcopy entries in `api/v1alpha1/zz_generated.deepcopy.go` (uncommitted) that do **not** match committed `mustgather_types.go`.

## 1. Architecture Overview

### 1.1 Project Type & Tech Stack

| Item | Detail |
|------|--------|
| **Type** | OpenShift/Kubernetes operator (controller-runtime, kubebuilder v4 layout) |
| **Language** | Go 1.24.0 |
| **Module** | `github.com/openshift/must-gather-operator` |
| **Framework** | `sigs.k8s.io/controller-runtime` v0.21.0 |
| **CRD group** | `operator.openshift.io/v1alpha1`, kind `MustGather` (namespaced) |
| **Build** | OpenShift boilerplate Makefile (`boilerplate/generated-includes.mk`) |
| **FIPS** | Enabled by default (`FIPS_ENABLED=true` in root `Makefile`) |

Evidence: `go.mod`, `PROJECT`, `api/v1alpha1/groupversion_info.go`, root `Makefile`.

### 1.2 Component Map

| Path | Responsibility | Hand-written |
|------|----------------|--------------|
| `api/v1alpha1/` | CRD Go types, kubebuilder markers | Yes |
| `api/v1alpha1/zz_generated.deepcopy.go` | controller-gen deepcopy | Generated |
| `controllers/mustgather/` | Reconcile loop, Job template, predicates | Yes |
| `build/bin/upload` | Shell script: tar + SFTP upload (runs in upload container) | Yes |
| `deploy/crds/` | CRD YAML | Generated via `make manifests` |
| `bundle/manifests/` | OLM bundle CRD copy | Generated |
| `config/` | Operator name/namespace constants | Yes |
| `pkg/localmetrics/` | Prometheus counters | Yes |
| `pkg/k8sutil/` | Operator namespace detection | Yes |
| `test/e2e/` | osde2e integration tests (`//go:build osde2e`) | Yes |
| `controllers/mustgather/*_test.go` | Unit tests (stdlib `testing`, in-package) | Yes |
| `examples/` | Sample MustGather YAML | Yes |
| `boilerplate/` | OpenShift convention system (do not edit directly) | Upstream |

### 1.3 Framework & Pattern Architecture

- **Single reconciler:** `MustGatherReconciler` embeds `util.ReconcilerBase` from `github.com/redhat-cop/operator-utils` for status/error helpers (`ManageSuccess`, `ManageError`).
- **Entry point:** Standard kubebuilder `main.go` (not re-read here; standard controller-runtime manager pattern).
- **Watches:** `For(MustGather)` with `resourceGenerationOrFinalizerChangedPredicate`; `Owns(Job)` with `isStateUpdated()` — prevents status-update reconcile loops on MustGather.
- **Job model:** Two containers (`gather`, `upload`) with `ShareProcessNamespace: true`; shared `emptyDir` volume `must-gather-output` at `/must-gather`.
- **Secret handling:** User secret in CR namespace is copied to operator namespace before Job creation; Job references operator-namespace secret.
- **Spec updates ignored:** Reconcile comment at line 283–285 states spec updates are unsupported; generation predicate filters status-only updates.

Evidence: `controllers/mustgather/mustgather_controller.go`, `controllers/mustgather/predicates.go`, `controllers/mustgather/template.go`.

### 1.4 Runtime Data/Control Flow

1. Administrator creates `MustGather` CR (currently **requires** `caseID` + `caseManagementAccountSecretRef`).
2. Reconciler initializes defaults (`serviceAccountRef=default`, `proxyConfig` from cluster `Proxy` if empty).
3. Reconciler adds finalizer `finalizer.mustgathers.operator.openshift.io`.
4. On first reconcile: copy case-management secret from CR namespace → operator namespace; create Job in **operator namespace** (not CR namespace).
5. Job pod runs:
   - **gather** container: runs `gather` or `gather_audit_logs` with timeout; writes to `/must-gather` (`emptyDir`).
   - **upload** container: waits for gather processes via `pgrep`, tars output, SFTP upload via `/usr/local/bin/upload`.
6. Upload script uses env vars (`caseid`, `username`, `password`, `internal_user`, proxy vars) and hardcoded `FTP_HOST=sftp.access.redhat.com`.
7. On Job success/failure: controller deletes resources via `DeleteResourceIfExists`; updates `status.completed`.

**MG-53 target flow (not yet implemented):** Upload only when upload destination configured; pass hostname from CR → controller env → upload script; reject legacy flat fields.

## 2. Target Files (Modification & Creation)

### API & Code Generation

| File | Action | Reason |
|------|--------|--------|
| `api/v1alpha1/mustgather_types.go` | Modify | Add `UploadTarget` union + `SFTPUploadTargetConfig`; remove flat `caseID`, `caseManagementAccountSecretRef`, move `internalUser` under SFTP config; add CEL XValidation. (confidence: high) |
| `api/v1alpha1/zz_generated.deepcopy.go` | Regenerate | `make generate` after type changes. (confidence: high) |
| `deploy/crds/operator.openshift.io_mustgathers.yaml` | Regenerate | `make manifests` — CRD OpenAPI + CEL rules. (confidence: high) |
| `bundle/manifests/tech-preview/operator.openshift.io_mustgathers.yaml` | Regenerate | Bundle CRD sync. (confidence: high) |

### Controller & Job Template

| File | Action | Reason |
|------|--------|--------|
| `controllers/mustgather/template.go` | Modify | Conditional upload container; read hostname/caseID/credentials from `uploadTarget.sftp`; pass SFTP host env var to upload container. (confidence: high) |
| `controllers/mustgather/mustgather_controller.go` | Modify | Gate Job creation on upload config validation; handle optional upload (gather-only Job); update secret copy logic for optional upload. (confidence: high) |
| `controllers/mustgather/template_test.go` | Modify | Tests for optional upload, custom hostname env, single-container Job when no uploadTarget. (confidence: high) |
| `controllers/mustgather/mustgather_controller_test.go` | Modify | Reconcile tests for new spec shapes. (confidence: medium) |

### Upload Script & Image

| File | Action | Reason |
|------|--------|--------|
| `build/bin/upload` | Modify | Replace hardcoded `FTP_HOST=sftp.access.redhat.com` with env var (e.g., `sftp_host`) defaulting to production host. (confidence: high) |

### Examples & Tests

| File | Action | Reason |
|------|--------|--------|
| `examples/mustgather_*.yaml` | Modify | Migrate to `uploadTarget` structure; add staging hostname example. (confidence: high) |
| `test/must-gather.yaml` | Modify | Update test fixture CR. (confidence: high) |
| `test/e2e/must_gather_operator_tests.go` | Modify | osde2e CR construction uses new API. (confidence: medium) |

## 3. Reference Context (Read-Only)

### 3.1 Entry Points & Wiring

- `controllers/mustgather/mustgather_controller.go` — `SetupWithManager`, `Reconcile`, `getJobFromInstance`
- `main.go` — manager bootstrap, env var wiring (`OPERATOR_IMAGE`, `DEFAULT_MUST_GATHER_IMAGE`)

### 3.2 API / Interface Patterns

- `api/v1alpha1/mustgather_types.go` — current flat spec (pre-feature)
- `api/v1alpha1/groupversion_info.go` — `operator.openshift.io/v1alpha1`
- EP in `openspec/changes/mg-53/inputs/jira-spec.md` — target union API with kubebuilder markers

### 3.3 Build, CI & Tooling

- Root `Makefile` → `boilerplate/generated-includes.mk`
- `boilerplate/openshift/golang-osd-operator/standard.mk` — `generate`, `manifests`, `go-test`, `validate`, `lint`
- `.ci-operator.yaml` — OpenShift CI boilerplate image `boilerplate:image-v8.2.0`

### 3.4 Manifest / Config Generation

- `make generate` — deepcopy, openapi, manifests
- `make manifests` — controller-gen CRD when `config/default` absent: runs `controller-gen object` + optional kustomize
- CRD output: `deploy/crds/operator.openshift.io_mustgathers.yaml`

### 3.5 Test Patterns & Fixtures

- Unit: `controllers/mustgather/template_test.go` — table-driven, in-package, `t.Setenv` for image env
- E2E: `test/e2e/must_gather_operator_tests.go` — Ginkgo, build tag `osde2e`, excluded from `make go-test`
- Examples: `examples/mustgather_basic.yaml`, `mustgather_proxy.yaml`, etc.

## 4. Configuration Surface & Runtime Behavior

### 4.1 Current Configuration Surface (MustGather spec — pre-MG-53)

| Field | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| `caseID` | string | **Yes** | — | Upload case number |
| `caseManagementAccountSecretRef` | LocalObjectReference | **Yes** | — | Secret with `username`/`password` keys |
| `serviceAccountRef` | LocalObjectReference | No | `default` (controller init) | Job pod SA |
| `audit` | bool | No | `false` | Use `gather_audit_logs` binary |
| `proxyConfig` | ProxySpec | No | Cluster `Proxy` if empty | httpProxy/httpsProxy required when set |
| `mustGatherTimeout` | Duration | No | none | Gather command timeout |
| `internalUser` | bool | No | `true` (CRD default) | SFTP remote path prefix behavior |

**Not present on branch:** `uploadTarget`, optional upload, SFTP `host` override, PVC retention fields.

Runtime env vars (operator Deployment):

| Variable | Required | Usage |
|----------|----------|-------|
| `DEFAULT_MUST_GATHER_IMAGE` | De facto yes | Gather container image via `os.Getenv` — **no empty guard** |
| `OPERATOR_IMAGE` | Yes | Upload container image; reconcile fails if missing |
| `OPERATOR_SERVICE_ACCOUNT` | Cluster deploy | Downward API |
| Proxy vars | No | Inherited for upload container when CR proxy empty |

### 4.2 Reconciliation / Processing Flow

| Step | Action | On error |
|------|--------|----------|
| 1 | Get MustGather CR | NotFound → exit; other → requeue |
| 2 | `IsInitialized` — default SA, populate proxy from cluster | Proxy get failure logged; may leave uninitialized |
| 3 | Handle deletion + finalizer cleanup (secret, pods, job) | Return error, requeue |
| 4 | Add finalizer if missing | Update error → ManageError |
| 5 | Build Job template via `getJobTemplate` | Missing `OPERATOR_IMAGE` → error |
| 6 | Get/create secret copy in operator namespace | Get/Create error → requeue |
| 7 | Create Job if not exists | Create error → ManageError |
| 8 | Monitor Job status | Active → log; Succeeded/Failed → delete resources |
| 9 | `updateStatus` — set `status.completed` | ManageSuccess |

**Upload is unconditional today** — step 5 always appends gather + upload containers.

### 4.3 Image / Dependency Resolution

- **Gather image:** `DEFAULT_MUST_GATHER_IMAGE` env at Job template time (`template.go:162`).
- **Upload image:** `OPERATOR_IMAGE` env read in `getJobFromInstance` (`mustgather_controller.go:345–349`).
- **Upload binary:** Baked into operator image at `/usr/local/bin/upload` from `build/bin/upload`.

### 4.4 Status / Health Reporting

- **Status fields:** `status`, `lastUpdate`, `reason`, `conditions[]`, `completed` (bool).
- **Condition types:** CRD supports standard `metav1.Condition` array; controller primarily uses `ManageSuccess`/`ManageError` from operator-utils rather than typed condition constants — **no dedicated `Validation*` / `Upload*` condition types** observed in controller code.
- **Metrics:** `localmetrics.MetricMustGatherTotal`, `MetricMustGatherErrors` incremented on create/failure.
- **Job failure handling:** Failed jobs trigger resource deletion and error metric increment; limited surfaced detail on CR status.

### 4.5 Feature Gate / Feature Flag Mechanism

* Not applicable — no feature gates in this operator.

## 5. Reusable Assets (Anti-Duplication)

| Asset | Use for | Evidence |
|-------|---------|----------|
| `getJobTemplate` / `initializeJobTemplate` | Job scaffolding (affinity, tolerations, volumes, SA) | `template.go:51–146` |
| `getGatherContainer` | Gather container spec with audit/timeout | `template.go:148–171` |
| `getUploadContainer` | Upload env wiring pattern (extend with hostname env) | `template.go:173–253` |
| `ToPtr[T]` | Pointer literals in specs | `template.go:255` |
| `resourceGenerationOrFinalizerChangedPredicate` | Avoid status-update reconcile loops | `predicates.go:36–50` |
| `util.ReconcilerBase` | Status patch helpers | `mustgather_controller.go` |
| `proxy.ReadProxyVarsFromEnv` | Operator-level proxy fallback | `template.go:66` |
| `build/bin/upload` | Tar + SFTP logic — extend, do not rewrite | Existing proxy + internal_user handling |
| `controllers/mustgather/template_test.go` | Unit test patterns for container env assertions | Existing tests |

## 6. Architectural Guardrails

### Structural

- Controller package is `mustgather` (not `mustgather_test`) for unit tests — follow in-package testing.
- Container names `gather` and `upload` are stable — E2E/tests reference them.
- Jobs created in **operator namespace**, not CR namespace.

### API / Schema

- API group is `operator.openshift.io` (not `managed.openshift.io` from EP examples).
- Breaking change: removing required `caseID` / `caseManagementAccountSecretRef` from top level — update all examples, tests, CRD required list.
- Use kubebuilder union + CEL XValidation per EP; follow `has()` guards for omitempty fields.
- Spec immutability: no spec update handling — migration = delete/recreate CR.

### Build / Tooling

- Run `make generate && make manifests` after API edits; commit generated files.
- FIPS build tag active — do not remove `fips.go` / `fips_enabled` tag without compliance review.
- `make validate` runs `generate-check` — generated files must match committed.

### Deployment / Packaging

- Upload script changes require operator image rebuild (upload binary copied into image).
- Update both `deploy/crds/` and `bundle/manifests/` CRD copies.

### Code Generation

- Never hand-edit `zz_generated.deepcopy.go`.
- CRD from controller-gen only — do not hand-edit OpenAPI sections.

### Security

- Secrets copied operator-namespace for Job mount — preserve pattern when upload optional.
- SFTP uses `sshpass` + `sftp` with `StrictHostKeyChecking=no` — existing behavior; hostname change must not weaken auth.
- Credential secret keys: `username`, `password` (upload container env from secretKeyRef).

## 7. Change Cascade Checklist

| When you change... | You must also... | Verify with... |
|---|---|---|
| `api/v1alpha1/mustgather_types.go` | Regenerate deepcopy, CRD, bundle CRD; update examples/tests | `make generate && make manifests && make validate` |
| CEL / union markers | Confirm controller-gen version supports markers in `PROJECT` | Inspect generated CRD for `x-kubernetes-validations` |
| `controllers/mustgather/template.go` | Update `template_test.go`; rebuild operator image if upload env changes | `go test ./controllers/mustgather/... -count=1` |
| `build/bin/upload` | Rebuild container image; update template_test if new env var | `go test ./controllers/mustgather/... -count=1` |
| CRD required fields | Update osde2e test CR in `test/e2e/` | `make go-test` (unit); osde2e in CI |
| Example YAML under `examples/` | Align with new API; document staging hostname | `make olm-deploy-yaml-validate` |
| RBAC (if new secret/config access) | Add `+kubebuilder:rbac` markers, regenerate deploy RBAC | `make manifests` |

## 8. Test & CI Reference

### 8.1 Test Structure

| Tier | Location | Framework |
|------|----------|-----------|
| Unit | `controllers/mustgather/*_test.go` | stdlib `testing`, in-package |
| E2E | `test/e2e/` | Ginkgo/Gomega, tag `osde2e` |
| API | No dedicated `api/..._test.go` for types | — |

### 8.2 How to Run Tests Locally

```bash
# Default preflight (from boilerplate)
make                    # go-check + go-test + go-build

# Unit tests only
make go-test

# Specific package
go test ./controllers/mustgather/... -count=1 -v

# Verify codegen clean
make validate

# Lint
make lint
```

Prerequisites: Go 1.24+, envtest assets downloaded automatically by `make go-test`.

### 8.3 CI Pipeline

- Config: `.ci-operator.yaml` → OpenShift CI (external `openshift/release` prow)
- Expected jobs: `validate` (generate-check), `lint` (yaml-validate + go-check), `test` (go-test)
- E2E: osde2e suite runs separately with `//go:build osde2e` — not in default unit test target

### 8.4 Test Coverage Gaps

- No unit tests for `mustgather_controller.go` reconcile paths (secret copy, job lifecycle).
- No unit test for `build/bin/upload` hostname behavior (shell — container/integration test needed).
- Upload/SFTP E2E requires live network — typically skipped in disconnected CI.
- **MG-53 gaps to fill:** CEL validation tests, optional-upload Job shape, custom hostname env propagation, breaking-change migration scenarios.

## 9. Developer Workflow

### 9.1 Key Commands Reference

| Command | Purpose |
|---------|---------|
| `make` | Default: check + test + build |
| `make generate` | deepcopy + openapi + manifests |
| `make manifests` | CRD regeneration |
| `make validate` | Ensure generated code committed |
| `make lint` | YAML + go-check |
| `make go-test` | Unit tests (excludes `test/e2e`) |
| `make go-build` | Build operator binary |
| `make coverage` | Coverage report |

**Preflight before PR:** `make validate && make lint && make go-test`

### 9.2 Version Variables

- Operator versioning via `VERSION_MAJOR`/`VERSION_MINOR` + git commit count (`project.mk`, `standard.mk`)
- Operand images: `DEFAULT_MUST_GATHER_IMAGE`, `OPERATOR_IMAGE` (runtime env, not Makefile vars)

### 9.3 Local Development Setup

1. `go mod download`
2. Apply CRD: `oc apply -f deploy/crds/operator.openshift.io_mustgathers.yaml`
3. Export env vars:
   ```bash
   export DEFAULT_MUST_GATHER_IMAGE='quay.io/openshift/origin-must-gather:latest'
   export OPERATOR_IMAGE='quay.io/openshift/origin-must-gather-operator:latest'
   ```
4. Run: `OPERATOR_NAME=must-gather-operator operator-sdk run --local --namespace ''`

Evidence: `openspec/inputs/agents.md` Local Development Checklist.

### 9.4 Common Development Scenarios

**How to add a new API field (uploadTarget pattern for MG-53):**

1. Edit `api/v1alpha1/mustgather_types.go` — add types, kubebuilder markers, CEL rules.
2. Run `make generate && make manifests`.
3. Update controller `template.go` to read new field; conditionally configure upload.
4. Update `build/bin/upload` if new env var needed (hostname).
5. Update `examples/` and unit tests in `template_test.go`.
6. Run `make validate && make go-test`.

**How to make upload optional:**

1. Remove unconditional `getUploadContainer` append in `getJobTemplate`.
2. Guard on `spec.uploadTarget != nil && spec.uploadTarget.Type == SFTP`.
3. Adjust CRD required fields — `caseID` no longer top-level required.
4. Update reconcile secret-copy to skip when upload disabled.

## 10. Platform & Environment Integration

### 10.1 Security Context & Permissions

- Cluster-scoped RBAC for Jobs, Pods, Secrets, ServiceAccounts across namespaces (operator watches all namespaces).
- Job uses CR's `serviceAccountRef` (defaults to `default`).

### 10.2 Proxy & Network Configuration

- CR-level `proxyConfig` overrides operator env proxy for upload container.
- Upload script builds `ProxyCommand` via `nc` when `http_proxy`/`https_proxy` set (`build/bin/upload:23–60`).
- SFTP connectivity to custom host must work through cluster proxy/firewall — staging hostname (MG-53) depends on network path.

### 10.3 Cloud Provider Integration

* Not applicable — SFTP upload to Red Hat support/staging servers; no cloud credential operator integration.

### 10.4 Build & Compliance Constraints

- FIPS: `FIPS_ENABLED=true`; `fips.go` imports `crypto/tls/fipsonly` under build tag.
- Multi-arch: standard OpenShift boilerplate docker-build patterns.

### 10.5 Console / UI Integration

* Not applicable — no console plugin in repo.

### 10.6 Packaging & Lifecycle

- OLM bundle under `bundle/`; CSV in `bundle/manifests/`.
- CRD updates must be shipped in bundle + `deploy/crds/` for downstream `openshift/must-gather-operator` consumers.

## 11. Risks & Downstream Impacts

- **Breaking API migration:** Removing required top-level fields breaks existing CRs, examples, osde2e tests, and any in-cluster MustGather resources. Impact: all consumers. Mitigation: document migration YAML; CRD validation rejects old shape immediately.

- **Hardcoded hostname in upload script:** `FTP_HOST=sftp.access.redhat.com` in `build/bin/upload:19` is the MG-53 root cause. Impact: staging/CI uploads always hit production until script + env wiring fixed. Mitigation: env-driven host with production default.

- **Unconditional upload container:** Current `getJobTemplate` always adds upload — conflicts with FR-006 (optional upload). Impact: cannot disable upload without code change. Mitigation: conditional container append.

- **Shared volume / multi-container consistency:** Gather and upload share `must-gather-output` `emptyDir` at `/must-gather`. Upload also uses separate `must-gather-upload` workspace. Any volume mount changes must apply to **both** containers sharing a volume. Impact: broken upload if mounts diverge. Mitigation: update `initializeJobTemplate` and both container mount lists together.

- **Data overwrite on repeated runs (PVC future):** Current branch uses `emptyDir` only — no PVC retention in controller. EP/spec mention PVC retention as future/operational behavior. If PVC support is added later without **per-invocation isolation** (subPath/subPathExpr), multiple runs could overwrite prior bundles. Impact: data integrity. Mitigation: use unique subPath per CR name/UID when introducing PVC.

- **Stale generated code in working tree:** Uncommitted `UploadTarget` deepcopy without matching `mustgather_types.go` can confuse builds. Mitigation: regenerate or revert before implementation branch.

- **Secret copy in operator namespace:** Upload-disabled gathers may still trigger secret copy today — refactor should skip unnecessary secret operations.

### 11.1 Assessment Limitations / UNVERIFIED Items

- `main.go` not re-read this session — verify manager env wiring and leader election flags if planning changes operator Deployment env.
- No `validation.go` in `controllers/mustgather/` on branch — `openspec/inputs/agents.md` references SFTP validation patterns that may be aspirational or on another branch; verify before planning validation hooks.
- PVC retention described in EP/spec **not implemented** in `template.go` (only `emptyDir` volumes) — marked UNVERIFIED for PVC/subPath behavior; per-invocation isolation not applicable until PVC lands.
- osde2e SFTP upload success path not verified locally — requires live cluster and network.
- Upstream canonical repo vs fork (`chirag-wrk`) — implementation targets working folder; upstream PR would go to `openshift/must-gather-operator`.

## 12. Quick Reference Card

### Preflight Checklist

```
1. make validate
2. make lint
3. make go-test
4. go test ./controllers/mustgather/... -count=1 -v   # after controller changes
```

### Key File Quick-Nav

| I want to... | Look at... |
|---|---|
| Add uploadTarget API types | `api/v1alpha1/mustgather_types.go` |
| Regenerate CRD | `make manifests` → `deploy/crds/operator.openshift.io_mustgathers.yaml` |
| Change Job/upload wiring | `controllers/mustgather/template.go` |
| Change reconcile flow | `controllers/mustgather/mustgather_controller.go` |
| Fix SFTP hostname | `build/bin/upload` + `getUploadContainer` env vars |
| Add unit tests | `controllers/mustgather/template_test.go` |
| Update sample CRs | `examples/mustgather_*.yaml` |
| Avoid reconcile loops | `controllers/mustgather/predicates.go` |
