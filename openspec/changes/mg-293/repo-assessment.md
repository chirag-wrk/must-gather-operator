# Repository Assessment Report
**Feature:** Must-Gather Bundle Obfuscation (MG-293)

## 0. Inputs & Tooling

| Field | Value |
|-------|-------|
| **repo** | https://github.com/chirag-wrk/must-gather-operator.git |
| **local root** | `/home/cdate/must-clean/must-gather-operator` |
| **branch** | `master` |
| **commit** | `501f600a7ce3c92c834793e035b169258590daa8` |
| **tooling_status** | OK |
| **spec status** | `validation.json` PASS (89%); `specs.md` approved |
| **feature on branch** | **NOT PRESENT** — greenfield implementation required |

Obfuscation (`obfuscate`, `must-gather-clean`, `runObfuscate`) does not exist on the pinned commit. Grep across Go/shell sources returns no matches. All MG-293 work is net-new on this branch.

## 1. Architecture Overview

### 1.1 Project Type & Tech Stack

- **Type:** Kubernetes/OpenShift operator (controller-runtime, kubebuilder markers, Operator SDK conventions)
- **Language:** Go (module `github.com/openshift/must-gather-operator`; builder image uses Go 1.25 / OCP 4.22)
- **Key dependencies:** controller-runtime, operator-lib (proxy, leader), operator-utils, openshift/operator-custom-metrics, OpenShift API (`config.openshift.io`, `image.openshift.io`)
- **Build:** OpenShift boilerplate Makefile (`boilerplate/generated-includes.mk`); `FIPS_ENABLED=true` by default
- **Runtime image:** RHEL 9 base; operator binary + `build/bin/` scripts at `/usr/local/bin/`; **runs as UID 65534** (`Dockerfile.openshift` line 18)

### 1.2 Component Map

| Package / path | Responsibility |
|----------------|----------------|
| `main.go` | Manager bootstrap, leader election, metrics, reconciler registration |
| `api/v1alpha1/` | MustGather CRD types, CEL validation, deepcopy (generated) |
| `controllers/mustgather/` | Reconcile loop, Job template, SFTP validation, predicates |
| `build/bin/upload` | Shell: tar + SFTP upload (runs inside upload container) |
| `pkg/localmetrics/` | Prometheus counters |
| `pkg/mustgatherutil/` | Directory naming for PVC subPath isolation |
| `pkg/k8sutil/` | Operator namespace detection |
| `deploy/` | CRD, Deployment, RBAC manifests |
| `test/e2e/` | Ginkgo E2E (build tag `e2e`) |
| `boilerplate/` | Generated Makefile includes — **do not edit directly** |

### 1.3 Framework & Pattern Architecture

- **Single reconciler:** `MustGatherReconciler` in `controllers/mustgather/mustgather_controller.go`
- **Watches:** MustGather CR (generation/finalizer predicate); owns Jobs (state-updated predicate); optionally owns trusted-CA ConfigMap
- **Spec immutability:** CRD CEL rule `self.spec == oldSelf.spec` — users must create a **new** CR to change configuration (`api/v1alpha1/mustgather_types.go` line 231)
- **Finalizer:** `finalizer.mustgathers.operator.openshift.io` — explicit Job/Pod/ConfigMap cleanup
- **Job model:** Up to two containers with `ShareProcessNamespace: true`:
  - **`gather`** — must-gather image; writes to `/must-gather`
  - **`upload`** — operator image (UID 65534); polls for gather via `pgrep`; runs `/usr/local/bin/upload`
- **Upload container is conditional:** Added only when `spec.uploadTarget.type == SFTP` with valid case ID and secret ref (`template.go` lines 130–151). Jobs without upload have gather only.
- **Dead-code trap:** Do not add obfuscation logic to unused code paths; the upload script and gather/upload container builders are the integration surface.

### 1.4 Runtime Data/Control Flow

**Standard gather + upload flow:**

1. User creates MustGather CR in any namespace
2. Reconciler validates ServiceAccount, optional ImageStream, optional SFTP credentials
3. On success, creates Job in CR namespace with owner reference
4. **Gather container** runs `/usr/bin/gather` or `/usr/bin/gather_audit_logs`; output → shared volume `must-gather-output` mounted at `/must-gather`
5. **Upload container** waits until `pgrep gather` finds no process (poll loop in `uploadCommand`, `template.go` line 52), then runs `/usr/local/bin/upload`
6. Upload script tars `$must_gather_output` → `$must_gather_upload/*.tar.gz`, SFTP to Red Hat
7. Controller monitors Job; updates `status.completed`, `status.status`, conditions on completion/failure

**Shared volume architecture (critical for obfuscation):**

| Volume | Mount path | Containers | Type |
|--------|------------|------------|------|
| `must-gather-output` | `/must-gather` | gather + upload | emptyDir or PVC |
| `must-gather-upload` | `/must-gather-upload` | upload only | emptyDir |

Both gather and upload mount `must-gather-output` at `/must-gather`. When `spec.storage` uses PVC, **`outputSubPath()`** computes `path.Join(baseSubPath, directoryName)` so each run writes to a **unique subPath** under the claim (`template.go` lines 59–68, 248–251, 324–327). Upload container receives `FILENAME_PREFIX` env set to the same `directoryName` (`template.go` lines 406–409).

**MG-293 insertion point (planned, not implemented):** Post-gather, pre-tar obfuscation inside upload container workflow, reading `/must-gather`, writing cleaned output to `/must-gather-upload/cleaned`, then repointing upload input.

## 2. Target Files (Modification & Creation)

### API & code generation

- `api/v1alpha1/mustgather_types.go`: Add `Obfuscate *ObfuscateConfig` to `MustGatherSpec`; new structs; CEL rules for FR-012/FR-013. (confidence: high)
- `deploy/crds/operator.openshift.io_mustgathers.yaml`: Regenerated via `make manifests`. (confidence: high)
- `bundle/manifests/operator.openshift.io_mustgathers.yaml`: Regenerated with bundle flow if OLM bundle updated. (confidence: high)

### Controller & Job template

- `controllers/mustgather/template.go`: Extend `getJobTemplate`, `getGatherContainer`, `getUploadContainer` for obfuscation env vars, optional gather omission, PVC source mount, ConfigMap mount, `chown` suffix on gather command. (confidence: high)
- `controllers/mustgather/mustgather_controller.go`: Pass obfuscation config into template; optional pre-Job validation for obfuscation refs; RBAC markers for ConfigMap read. (confidence: high)
- `controllers/mustgather/constant.go`: New env var name constants for obfuscation. (confidence: high)
- `controllers/mustgather/template_test.go`: Table-driven tests for obfuscation branches (existing pattern). (confidence: high)

### Operator binary & upload pipeline

- `main.go`: Add Cobra subcommand or flag path `obfuscate --input --output [--config]` invoking must-gather-clean. (confidence: high)
- `build/bin/upload`: Conditional obfuscation block before tar step. (confidence: high)
- `build/obfuscate-config.yaml`: **(New)** Default obfuscation policy baked into image. (confidence: high)
- `Dockerfile.openshift` / `build/Dockerfile`: COPY default config into image at fixed path. (confidence: high)
- `go.mod` / `go.sum`: Add `github.com/openshift/must-gather-clean` dependency. (confidence: high)

### Tests & examples

- `controllers/mustgather/mustgather_controller_test.go`: Extend if reconcile validates obfuscation config. (confidence: medium)
- `test/e2e/`: New or extended scenarios for obfuscated upload. (confidence: medium)
- `examples/`: Sample CRs with `obfuscate.enabled`. (confidence: medium)

### Do not edit without cause

- `vendor/` — updated via `go mod vendor` after dependency add
- `boilerplate/` — use `make boilerplate-update` only
- Generated `zz_generated.deepcopy.go` — use `make generate`

## 3. Reference Context (Read-Only)

### 3.1 Entry Points & Wiring

- `main.go` — manager, env var gates (`DEFAULT_MUST_GATHER_IMAGE`, `OPERATOR_IMAGE`, `OPERATOR_SERVICE_ACCOUNT`)
- `controllers/mustgather/mustgather_controller.go` — `SetupWithManager`, `Reconcile`, `getJobFromInstance`

### 3.2 API / Interface Patterns

- `api/v1alpha1/mustgather_types.go` — existing `Storage`, `UploadTargetSpec`, CEL patterns to mirror for `ObfuscateConfig`
- `docs/api-contracts-guidelines.md` — CEL, immutability, union discriminators

### 3.3 Build, CI & Tooling

- `Makefile` — `make`, `make go-test`, `make test-e2e`, `make generate`, `make manifests`, `make lint`
- `Dockerfile.openshift` — production image layout
- `.ci-operator.yaml` — OpenShift CI configuration

### 3.4 Manifest / Config Generation

- `deploy/crds/` — CRD source of truth post-`make manifests`
- kubebuilder RBAC markers in controller → `deploy/` RBAC YAML

### 3.5 Test Patterns & Fixtures

- `controllers/mustgather/template_test.go` — in-package unit tests for Job template functions
- `controllers/mustgather/mustgather_controller_test.go` — `interceptClient`, condition assertions (`ReconcileError` / `ValidationFailed`)
- `test/e2e/testdata/` — embedded YAML fixtures
- `examples/` — reference MustGather CRs

## 4. Configuration Surface & Runtime Behavior

### 4.1 Current Configuration Surface

**MustGatherSpec fields (existing on `master`):**

| Field | Type | Default / constraint |
|-------|------|----------------------|
| `serviceAccountName` | string | **Required** |
| `imageStreamRef` | `{name, tag}` | Optional; operator-namespace ImageStream |
| `gatherSpec` | object | Optional; audit/command/args/since/sinceTime with CEL mutual exclusion |
| `mustGatherTimeout` | duration | Optional |
| `uploadTarget` | union (SFTP) | Optional; SFTP requires `sftp` member |
| `retainResourcesOnCompletion` | bool | Default `false` |
| `storage` | PersistentVolume | Optional; claim in CR namespace; optional `subPath` |

**Planned obfuscation fields (MG-293 — not on branch):**

| Field | Purpose |
|-------|---------|
| `obfuscate.enabled` | Activate redaction |
| `obfuscate.obfuscationConfigRef` | ConfigMap in operator namespace, key `config.yaml` |
| `obfuscate.source.claim` | PVC for existing bundle (operator namespace per proposal) |
| `obfuscate.source.subPath` | Optional path within PVC |

**Environment variables (operator startup — `main.go`, `constant.go`):**

| Variable | Required | Consumption |
|----------|----------|-------------|
| `DEFAULT_MUST_GATHER_IMAGE` | Yes | Default gather image if no ImageStreamRef |
| `OPERATOR_IMAGE` | Yes (Job creation) | Upload container image |
| `OPERATOR_SERVICE_ACCOUNT` | Yes (cluster) | Operator SA discovery |
| `OPERATOR_NAMESPACE` | No | Namespace detection |
| `OSDK_FORCE_RUN_MODE=local` | No | Bypass leader election |

**Upload container env (set in `getUploadContainer`):** `username`, `password`, `caseid`, `host`, `must_gather_output`, `must_gather_upload`, `internal_user`, proxy vars, `FILENAME_PREFIX`.

**PVC subPath / per-run isolation:** When `spec.storage.persistentVolume` is set, each Job run uses `outputSubPath(storage, directoryName)` → `{storage.subPath}/{directoryName}`. Prevents **data overwrite across multiple runs** on shared PVCs. `directoryName` generated by `mustgatherutil.GenerateMustGatherDirectoryName()` (evidence: `mustgather_controller.go` line 412, `template_test.go` `Test_outputSubPath`).

### 4.2 Reconciliation / Processing Flow (Detailed)

| Step | Action | On error |
|------|--------|----------|
| 1 | Fetch MustGather | NotFound → exit; other → requeue |
| 2 | Deletion? → finalizer cleanup | Cleanup errors → requeue |
| 3 | Add finalizer if missing | Update error → requeue |
| 4 | Validate ServiceAccount exists | `setValidationFailureStatus` → Failed, Completed=true |
| 5 | Resolve image (default env or ImageStream) | Image validation → Failed status |
| 6 | Validate SFTP if uploadTarget set | SFTP credential/protocol validation → Failed |
| 7 | Create Job if absent | Create error → ManageError |
| 8 | Monitor Job status | Failed Job → error metrics, status update |
| 9 | Success → Completed status | — |

**Upload script pipeline (`build/bin/upload`):** validate env → tar input dir → SFTP put. No obfuscation step today. Exit non-zero on missing params or upload failure.

### 4.3 Image / Dependency Resolution

- **Gather image:** `DEFAULT_MUST_GATHER_IMAGE` env at operator startup, or ImageStream tag from operator namespace
- **Upload/operator image:** `OPERATOR_IMAGE` env read at Job creation (`mustgather_controller.go` lines 404–410)
- **MG-293:** `must-gather-clean` library vendored into operator binary — no separate obfuscation image

### 4.4 Status / Health Reporting

**Status fields:** `status` (string), `completed` (bool), `reason`, `lastUpdate`, `conditions[]`.

**Condition type in use:** `ReconcileError` with reasons including `ValidationFailed` (`mustgather_controller.go` lines 352–358). Validation types tracked via string constants: `ValidationServiceAccount`, `ValidationSFTPCredentials`, `ValidationImageStream`, `ProtocolSFTP` (`constant.go`).

**No obfuscation-specific conditions** on branch; MG-293 non-goal (A-002) defers `Obfuscating` phase to future work.

**Default image env guard:** Operator exits at startup if `DEFAULT_MUST_GATHER_IMAGE` unset (`main.go` lines 156–160). Job creation fails if `OPERATOR_IMAGE` unset.

### 4.5 Feature Gate / Feature Flag Mechanism

* Not applicable — no feature gates in this operator. Obfuscation will be opt-in via CR field only.

## 5. Reusable Assets (Anti-Duplication)

- **`getJobTemplate()` / `initializeJobTemplate()`** (`template.go`): Reuse for volume layout, affinity, tolerations, trusted-CA wiring — extend rather than duplicate Job spec.
- **`outputSubPath()`** (`template.go`): Reuse for PVC path computation; obfuscation source mode should align with existing subPath patterns for **per-invocation isolation**.
- **`getUploadContainer()`** (`template.go`): Extend env/volume mounts for obfuscation ConfigMap and source PVC.
- **`upload` script** (`build/bin/upload`): Extend pre-tar hook; do not duplicate tar/SFTP logic.
- **`setValidationFailureStatus()`** (`mustgather_controller.go`): Reuse for admission-style validation failures (invalid obfuscation combo, missing ConfigMap at reconcile time if added).
- **`ToPtr[T]()`** (`template.go`): Pointer helpers for spec defaults.
- **`proxy.ReadProxyVarsFromEnv()`** (operator-lib): Already used for upload container proxy env.
- **`mustgatherutil.GenerateMustGatherDirectoryName()`**: Unique directory naming — reuse for gather output paths.
- **Package-level `var` hooks for network I/O** (`validation.go` pattern per `agents.md`): Follow when adding testable obfuscation boundaries.

**New dependency (to add):** `github.com/openshift/must-gather-clean` — call library `Run()` from new `obfuscate` subcommand; do not reimplement regex/IP/MAC logic.

## 6. Architectural Guardrails

### Structural

- Jobs created in **CR namespace**, not operator namespace (`agents.md`).
- Container names **`gather`** and **`upload`** are stable — E2E tests depend on them.
- Upload container only present when `uploadTarget` set — code must handle gather-only and upload-only (source mode) Job shapes.
- **Shared volume mounts must stay consistent** across all containers using `must-gather-output`: any new mount options (subPath, readOnly) must be applied to both gather and upload when both present, or documented when gather is omitted.

### API / Schema

- Spec is **immutable** after creation — obfuscation modes requiring spec changes need new CRs.
- Add CEL rules on `MustGatherSpec` for obfuscation field combinations (FR-012, FR-013).
- Backward compatible: `obfuscate` omitempty, nil default.

### Build / Tooling

- Run `make generate && make manifests` after API changes.
- FIPS enabled by default — do not break `fips.go` / build tags.
- Operator image runs as **UID 65534**; gather runs as root — obfuscation requires **chown** or equivalent (proposal pattern).

### Deployment / Packaging

- Default obfuscation config shipped **inside operator image** at fixed path.
- `build/bin/` copied to `/usr/local/bin/` in image (`Dockerfile.openshift` line 16).

### Code Generation

- Never hand-edit `zz_generated.deepcopy.go` or generated CRD without running generators.

### Security

- Upload container non-root (65534); gather root-owned files need ownership transfer before obfuscation reads/writes.
- Custom obfuscation ConfigMap in **operator namespace** — controller needs read RBAC; Job pod needs mount access.
- SFTP credentials remain in CR-namespace Secret refs — unchanged pattern.

## 7. Change Cascade Checklist

| When you change... | You must also... | Verify with... |
|---|---|---|
| `api/v1alpha1/mustgather_types.go` fields | `make generate` (deepcopy), `make manifests` (CRD + RBAC) | `make generate && make manifests` |
| kubebuilder RBAC markers | Regenerate deploy RBAC | `make manifests` |
| `go.mod` new dependency | `go mod vendor`, rebuild image | `make go-build` |
| `build/bin/upload` | Rebuild container image | `make docker-build` or CI |
| Default obfuscation YAML | Update Dockerfile COPY path | Image build |
| Job template volumes/mounts | Update **both** gather and upload mount helpers when sharing output volume | `go test ./controllers/mustgather/...` |
| CEL validation rules | Update CRD YAML + bundle manifests | `make manifests` |
| Examples / docs CRs | Keep in sync with new API fields | Manual review |

## 8. Test & CI Reference

### 8.1 Test Structure

- **Unit:** `controllers/mustgather/*_test.go` — stdlib `testing`, in-package
- **E2E:** `test/e2e/` — Ginkgo/Gomega, `//go:build e2e`
- **Fixtures:** `test/e2e/testdata/`, `examples/`

### 8.2 How to Run Tests Locally

```bash
make go-test          # Unit tests (excludes e2e)
make test-e2e         # E2E — requires KUBECONFIG + live cluster
make lint             # Linters
make coverage         # Coverage report
make                  # Default: build + test + lint
```

**E2E env:** `KUBECONFIG`, cluster with MustGather CRD installed; SFTP tests may use `[Skipped:Disconnected]` tag.

### 8.3 CI Pipeline

- OpenShift CI via `.ci-operator.yaml`; build uses `registry.ci.openshift.org/ocp/builder:rhel-9-golang-1.25-openshift-4.22`
- PR gate: `make` equivalent (build, test, lint) per `agents.md`

### 8.4 Test Coverage Gaps

- **No obfuscation tests** (feature absent)
- **`main.go` obfuscate subcommand** — will need new unit/integration tests
- **E2E obfuscation** — likely new scenarios; SFTP verification pattern exists for upload path
- Upload script — shell; tested indirectly via E2E today

## 9. Developer Workflow

### 9.1 Key Commands Reference

| Command | Purpose |
|---------|---------|
| `make` | Build + test + lint |
| `make go-build` | Operator binary |
| `make go-test` | Unit tests |
| `make generate` | Deepcopy / codegen |
| `make manifests` | CRD + RBAC YAML |
| `make docker-build` | Container image |
| `make test-e2e` | Cluster E2E |
| `make lint` | Static analysis |

**Preflight:** `make generate && make manifests` (if API changed) → `make go-test` → `make lint`

### 9.2 Version Variables

- Go/OCP versions pinned in `Dockerfile.openshift` builder image
- `FIPS_ENABLED=true` in Makefile
- Operand images via `DEFAULT_MUST_GATHER_IMAGE`, `OPERATOR_IMAGE` env — not in go.mod

### 9.3 Local Development Setup

Per `agents.md` and `CLAUDE.md`:

```bash
go mod download
oc apply -f deploy/crds/operator.openshift.io_mustgathers.yaml
oc new-project must-gather-operator
export DEFAULT_MUST_GATHER_IMAGE='quay.io/openshift/origin-must-gather:latest'
export OPERATOR_IMAGE='<your-operator-image>'
OPERATOR_NAME=must-gather-operator operator-sdk run --verbose --local --namespace ''
```

### 9.4 Common Development Scenarios

**How to add a new API field (obfuscation pattern):**

1. Add struct fields + kubebuilder/CEL markers in `api/v1alpha1/mustgather_types.go`
2. `make generate && make manifests`
3. Thread field through `getJobFromInstance` → `getJobTemplate` → container builders
4. Add table tests in `template_test.go`
5. Add example YAML under `examples/`

**How to add upload-pipeline behavior:**

1. Extend `build/bin/upload` for new pre-tar steps
2. If needs Go logic, add subcommand in `main.go` and invoke from upload script
3. Set env vars in `getUploadContainer`
4. Rebuild operator image

**How to add PVC-backed source mode:**

1. Reuse `PersistentVolumeClaimReference` / `outputSubPath` patterns from existing `storage` field
2. Conditionally omit gather container in `getJobTemplate`
3. Mount source PVC at `/must-gather` in upload container only
4. Document **per-invocation isolation** if reusing shared PVC without `directoryName` subPath

## 10. Platform & Environment Integration

### 10.1 Security Context & Permissions

- Operator Deployment: restricted SA; cluster-scoped RBAC for Jobs/Pods/Secrets cross-namespace
- Job upload container: **UID 65534** (image USER directive)
- Gather container: implicit root from must-gather image — **permission gap** for obfuscation (address via gather `chown` per proposal)
- MG-293 may need RBAC: `configmaps` get/list/watch in operator namespace for custom policy

### 10.2 Proxy & Network Configuration

- Operator inherits cluster proxy env; copied to upload container via `proxy.ReadProxyVarsFromEnv()` in `getJobTemplate`
- Upload script supports HTTP/HTTPS proxy for SFTP (`build/bin/upload` lines 25–65)

### 10.3 Cloud Provider Integration

* Not applicable — operator uses user-provided SFTP credentials, not cloud CCO.

### 10.4 Build & Compliance Constraints

- **FIPS:** `fips.go` with `fips_enabled` build tag; default `FIPS_ENABLED=true`
- Multi-arch via OCP build pipeline (not verified locally)

### 10.5 Console / UI Integration

* Not applicable — CR-only API; openshift-docs out of repo scope.

### 10.6 Packaging & Lifecycle

- OLM bundle under `bundle/`; CSV in `bundle/manifests/`
- CRD upgrade: optional field `obfuscate` — backward compatible
- Downgrade: older operator ignores unknown fields if CRD still includes them

## 11. Risks & Downstream Impacts

- **Permission gap (gather root → upload 65534):** Obfuscation reads/writes under `/must-gather`. Impact: EPERM failures. Mitigation: append `chown -R 65534:65534 /must-gather` to gather command when obfuscation enabled (proposal).
- **must-gather-clean fatal exits:** Library may `klog.Exitf` on file errors. Impact: upload container hard fail, Job failed. Mitigation: document; no partial upload (FR-014).
- **Staging disk pressure:** Cleaned output doubles space on `must-gather-upload` emptyDir. Impact: large bundles OOM/disk full. Mitigation: size limits, node disk monitoring.
- **PVC source mode without per-invocation isolation:** Reusing same PVC subPath for multiple obfuscation CRs could cause **data overwrite** or read wrong bundle. Impact: data integrity. Mitigation: use unique subPaths (existing `directoryName` pattern) or read-only source mount; verify in plan. **UNVERIFIED on branch:** no source-mode implementation yet.
- **Obfuscate-only output on emptyDir:** Staging lost when pod terminates. Impact: administrators cannot retrieve cleaned bundle (specs A-005). Mitigation: define durable sink in plan (PVC write-back or reject obfuscate-only without upload for Tech Preview).
- **Custom ConfigMap runtime-only validation:** Malformed policy fails at Job time. Impact: late failure UX. Mitigation: clear Job logs + MustGather Failed status.
- **CEL + immutability:** Invalid combos must fail at create time where possible; users cannot patch spec to fix — must recreate CR.

### 11.1 Assessment Limitations / UNVERIFIED Items

- **`must-gather-clean` API surface** — not vendored yet; verify `Run()` signature and worker config by reading upstream repo at plan time.
- **Obfuscation subcommand in `main.go`** — greenfield; CLI framework (cobra vs flags) not present today — verify approach during planning.
- **Source PVC namespace** — proposal says operator namespace; existing `storage.claim` uses CR namespace — confirm FR alignment during plan.
- **Node affinity for worker-only scheduling** — proposal mentions worker scheduling; current template prefers infra nodes (`node-role.kubernetes.io/infra`) — potential conflict to resolve in plan.
- **E2E SFTP infrastructure** — not exercised in this assessment; verify disconnected vs connected test tags.
- **Bundle/CSV updates** — OLM manifest bump scope not verified file-by-file.

## 12. Quick Reference Card

### Preflight Checklist (run before every PR)

```
1. make generate && make manifests   # if api/v1alpha1 changed
2. make go-test
3. make lint
4. make go-build
```

### Key File Quick-Nav

| I want to... | Look at... |
|---|---|
| Add obfuscation API fields | `api/v1alpha1/mustgather_types.go` |
| Change Job / container spec | `controllers/mustgather/template.go` |
| Change reconcile / validation | `controllers/mustgather/mustgather_controller.go` |
| Add pre-upload processing | `build/bin/upload` + `main.go` |
| Add default obfuscation policy | `build/obfuscate-config.yaml` (new) + `Dockerfile.openshift` |
| Add unit tests for template | `controllers/mustgather/template_test.go` |
| Regenerate CRD | `make manifests` → `deploy/crds/` |
| Add E2E scenario | `test/e2e/` + `test/e2e/testdata/` |
| Operator conventions | `openspec/inputs/agents.md` |
