# Technical Implementation Plan
**Feature:** Must-Gather Bundle Obfuscation (MG-293)

## 0. Inputs acknowledged

| Input | Status |
|-------|--------|
| Spec source | MG-293 — `specs.md` (approved) |
| Repo assessment pin | https://github.com/chirag-wrk/must-gather-operator.git, branch `master`, commit `501f600a7ce3c92c834793e035b169258590daa8` (tooling_status: OK) |
| `agents.md` | PROVIDED — `openspec/inputs/agents.md` (routing hints; defer to repo-assessment on architecture conflicts per constitution Governance) |
| `spec_validator_results.json` | PROVIDED — `validation.json` (PASS 89%) |
| `constitution.md` | PROVIDED — `openspec/inputs/constitution.md` v1.0.0 |
| **AgentRoutingMode** | PROVIDED |

## 1. Architectural strategy

Obfuscation integrates as a **post-gather, pre-upload** step inside the existing **upload container**, invoking a new **`obfuscate` subcommand** on the operator binary that wraps the vendored `must-gather-clean` library. No new container images or sidecars are introduced (FR-015). The controller extends the Job template and upload shell script; API changes are additive and backward compatible.

Three operational modes from `specs.md`:

1. **Gather + obfuscate + upload** — default P1 path; gather runs, upload obfuscates then SFTPs.
2. **Obfuscate + upload (source PVC)** — skip gather; read existing bundle from referenced PVC.
3. **Obfuscate only (source PVC, no upload)** — skip gather and SFTP; produce cleaned output on staging volume (Tech Preview: document retrieval limitations per specs A-005).

### Repo-grounded reality check

Repo-assessment §0 confirms **greenfield on pinned branch `501f600`**: no `obfuscate` field, no `must-gather-clean` dependency, no `runObfuscate` subcommand. Planning assumes **net-new implementation**, not hardening of existing obfuscation code.

Existing patterns to extend (repo-assessment §1.4, §5):

- Two-container Job with **`ShareProcessNamespace`** and shared `must-gather-output` volume at `/must-gather`.
- Upload container uses operator image at **UID 65534**; gather runs as root — **chown** required before obfuscation (proposal + repo-assessment §11).
- **`outputSubPath(storage, directoryName)`** already provides **per-invocation isolation** on PVC-backed storage via dynamic `directoryName` from `mustgatherutil.GenerateMustGatherDirectoryName()` — not a static subPath alone.
- Upload container is **conditional** on `uploadTarget`; source-only modes must omit gather and may run upload-only Job shape.

Constitution note: constitution.md III references EmptyDir-only volumes on an older pin; current branch supports **`spec.storage.persistentVolume`** — repo-assessment wins for PVC facts.

## 2. Persistence & state

### Kubernetes objects (source of truth)

| Object | Role |
|--------|------|
| `MustGather` CR | Desired obfuscation config (`obfuscate.enabled`, `obfuscationConfigRef`, `obfuscate.source`); immutable after create |
| `Job` / `Pod` | Ephemeral execution; obfuscation runs inside upload container lifecycle |
| ConfigMap (custom policy) | Optional; referenced by name in operator namespace |
| PVC (source or storage) | Existing bundle input (`obfuscate.source`) or gather output persistence (`spec.storage`) |

### Per-invocation isolation analysis (PVC)

**Problem:** Multiple MustGather CRs may reference the same PVC (storage or source). Static `subPath` alone causes **data overwrite** across runs (OCPBUGS-64626 pattern).

**Existing uniqueness strategy (gather + storage mode):** `outputSubPath()` joins `storage.persistentVolume.subPath` with a **unique `directoryName`** per Job (`path.Join(base, directoryName)`). Evidence: `template.go` `outputSubPath`, `mustgather_controller.go` `GenerateMustGatherDirectoryName`.

**Plan for obfuscation source mode:**

- **Read-only source mount:** Mount source PVC at `/must-gather` with user-provided `obfuscate.source.subPath` (optional). Source data MUST NOT be written by obfuscation (FR-010).
- **Write isolation for cleaned output:** Write obfuscated output to **`/must-gather-upload/cleaned`** (emptyDir staging), never back to source subPath.
- **Concurrent write safety:** Each Job gets its own pod emptyDir for staging; PVC source mounts are read-only where possible. For `spec.storage` gather mode, continue **`directoryName`-scoped subPath** so concurrent MustGather CRs do not clobber each other's gather directories.
- **Retention/cleanup:** Existing operator garbage collection (~6 hours post-completion) and finalizer cleanup apply. Obfuscation does not introduce new PVC retention semantics; old subdirectories on shared PVCs remain until administrator deletes them (document in examples/docs).

### Operand config/state

- Default obfuscation policy: file baked into operator image (`build/obfuscate-config.yaml` → `/etc/must-gather-clean/default-config.yaml`).
- Custom policy: ConfigMap key `config.yaml` mounted into upload pod when `obfuscationConfigRef` set.
- Upload env vars: `obfuscate=true`, optional `obfuscate_config` path; existing `must_gather_output`, `must_gather_upload`.

### External/platform-injected state

- Trusted CA ConfigMap pattern unchanged for SFTP/proxy paths.
- No new cluster-scoped ConfigMaps required beyond user-supplied obfuscation policies.

## 3. Interfaces & contracts (operator-native)

### 3.1 Kubernetes APIs (CRDs/CRs)

Extend `MustGatherSpec` (`api/v1alpha1/mustgather_types.go`):

```text
obfuscate.enabled              *bool
obfuscate.obfuscationConfigRef *LocalObjectReference  (operator namespace)
obfuscate.source.claim         PersistentVolumeClaimReference
obfuscate.source.subPath       string (optional)
```

**CEL rules (new):**

- Reject `obfuscate.enabled=true` when neither `obfuscate.source` nor `uploadTarget` present (FR-012).
- Reject `obfuscate.source` when `obfuscate.enabled` is not true (FR-013).

**Immutability:** Existing CRD rule `self.spec == oldSelf.spec` — users create new CRs per mode change.

**Backward compatibility:** `obfuscate` omitempty; nil preserves current behavior (FR-011).

### 3.2 Controller/runtime interfaces (internal)

| Interface | Contract |
|-----------|----------|
| `getJobTemplate(...)` | Accept obfuscation config; omit gather when `obfuscate.source` set; add ConfigMap volume for custom policy |
| `getGatherContainer(...)` | When obfuscation enabled and gather present: append `chown -R 65534:65534 /must-gather` to command |
| `getUploadContainer(...)` | Set obfuscation env vars; mount source PVC or shared output volume consistently |
| `runObfuscate` (`main.go`) | CLI: `--input`, `--output`, `--config` (default baked-in path); invoke `mgclean.Run()`; write `obfuscation.log` |
| Reconcile validation | Optional pre-Job checks for ConfigMap existence (runtime validation primary per A-007) |

**Status conditions (constitution II):** Add **distinct condition types** for obfuscation failure modes instead of overloading generic `ReconcileError` where possible:

| Condition type | When |
|----------------|------|
| `ObfuscationConfigInvalid` | Referenced ConfigMap missing or malformed at Job setup |
| `ObfuscationFailed` | Job/upload container exited after obfuscation failure |
| `ValidationFailed` | Existing pattern for invalid CR combo (may reuse `ReconcileError`/`ValidationFailed` reason for CEL-adjacent reconcile checks) |

All conditions MUST flow through `ManageError`/`ManageSuccess` (constitution I–II).

### 3.3 Webhooks / admission (if applicable)

N/A — Tech Preview uses CRD CEL validation only; no admission webhook (specs A-007). Runtime validation in Job remains authoritative for ConfigMap content.

### 3.4 RBAC / security boundaries (if applicable)

- Controller: add `configmaps` get/list/watch in operator namespace for custom policy resolution.
- Job pod: ServiceAccount from `spec.serviceAccountName` must read source PVC and mounted ConfigMap in CR namespace / operator namespace per final design.
- Gather **root → upload 65534** permission bridge via chown (not upload-as-root).
- SFTP credentials pattern unchanged (Secret in CR namespace).

### 3.5 Packaging / OLM (if applicable)

- Regenerate CRD in `deploy/crds/` and `bundle/manifests/` via `make manifests`.
- Operator image: include default obfuscation YAML and expanded binary (`Dockerfile.openshift`, `build/Dockerfile`).
- Tech Preview: document feature in CSV description if required by release process; no separate feature gate CR.

## 4. Dependencies & sequencing graph

### Critical path summary

1. API types + CEL + `make generate && make manifests`
2. Vendor `must-gather-clean` + `obfuscate` subcommand in `main.go`
3. Job template + upload script integration (depends on CLI)
4. Default config + Dockerfile COPY
5. Unit tests (`template_test.go`, obfuscate command tests)
6. Examples + optional E2E extension
7. Bundle/manifest refresh

### Parallelizable workstreams

- **Stream A:** API/codegen (Phase 1)
- **Stream B:** Default config YAML + Dockerfile (after config path finalized) — parallel with Stream C once CLI flags known
- **Stream C:** `build/bin/upload` script changes — after CLI contract defined
- **Stream D:** Examples/docs — after API stable

### Explicit blockers / external dependencies

- `github.com/openshift/must-gather-clean` module availability and `Run()` API (verify at implementation start).
- E2E SFTP infrastructure for end-to-end obfuscated upload verification.

## 5. Implementation phases (logical sequence; NOT tasks)

### Phase 1: API schema and code generation

- **Goal:** Introduce `ObfuscateConfig` types and CEL validation; regenerate deepcopy/CRD/OpenAPI.
- **Dependencies:** None.
- **Target files:** `api/v1alpha1/mustgather_types.go`; generated `zz_generated.deepcopy.go`; `deploy/crds/operator.openshift.io_mustgathers.yaml`; bundle CRD copies.
- **Required capabilities:** api-types, manifests-rbac (constitution Agent Routing).
- **Verification hooks:** `make generate && make manifests`; `make validate`; CRD contains new fields and CEL rules.

### Phase 2: Obfuscation library integration and CLI entrypoint

- **Goal:** Vendor `must-gather-clean`; implement `must-gather-operator obfuscate` with input/output/config flags and audit log output.
- **Dependencies:** Phase 1 (stable config path constants optional).
- **Target files:** `main.go`; `go.mod`/`go.sum`/`vendor/`; new package UNVERIFIED if split (e.g., `pkg/obfuscate/`) — discover during implementation.
- **Required capabilities:** controller-reconcile (binary entry), tests.
- **Verification hooks:** Unit tests for `runObfuscate` with fixture bundle directory; `make go-build`.

### Phase 3: Job template — multi-container volume consistency

- **Goal:** Wire obfuscation into Job spec: env vars, gather chown, optional gather omission, ConfigMap and source PVC mounts.
- **Dependencies:** Phases 1–2.
- **Target files:** `controllers/mustgather/template.go`; `controllers/mustgather/constant.go`; `controllers/mustgather/mustgather_controller.go`.
- **Required capabilities:** job-template, controller-reconcile.
- **Verification hooks:** `controllers/mustgather/template_test.go` — **gather container** and **upload container** both mount **shared volume** at `/must-gather` with matching subPath when PVC used; verify **both containers** receive consistent mount config; **edge case** tests for empty/malformed `obfuscate.source.subPath` and missing ConfigMap ref; `make go-test`.

**Consistency requirement:** Any change to `outputVolumeName`/`volumeMountPath` or subPath logic MUST update **`getGatherContainer` and `getUploadContainer`** together (constitution III).

### Phase 4: Upload pipeline hook

- **Goal:** Invoke obfuscation before tar/SFTP when `obfuscate=true`; repoint `must_gather_output` to cleaned staging dir.
- **Dependencies:** Phase 2 CLI, Phase 3 env wiring.
- **Target files:** `build/bin/upload`.
- **Required capabilities:** upload-script.
- **Verification hooks:** Shell-level test or integration test; manual Job log check for "Running obfuscation" / "Obfuscation complete".

### Phase 5: Default policy and image packaging

- **Goal:** Ship default obfuscation config in operator image; rebuild upload/gather image chain.
- **Dependencies:** Phase 2 config path.
- **Target files:** `build/obfuscate-config.yaml` (new); `Dockerfile.openshift`; `build/Dockerfile`.
- **Required capabilities:** manifests-rbac.
- **Verification hooks:** Image inspect for config file path; `make docker-build`.

### Phase 6: Reconcile validation, status conditions, and RBAC

- **Goal:** Distinct **condition types** for obfuscation/config failures; RBAC markers for ConfigMap read; guarded env usage for new required vars.
- **Dependencies:** Phases 1, 3.
- **Target files:** `controllers/mustgather/mustgather_controller.go`; RBAC via kubebuilder markers → `make manifests`.
- **Required capabilities:** controller-reconcile, manifests-rbac.
- **Verification hooks:** Unit tests asserting `ObfuscationConfigInvalid` / `ObfuscationFailed` conditions; **environment variable** **default** paths — **fail-fast** when `OPERATOR_IMAGE` missing (existing); document obfuscation env injection is conditional (only when enabled).

### Phase 7: Test coverage and examples

- **Goal:** Cover three modes in unit tests; add example CRs; extend E2E where cluster/SFTP available.
- **Dependencies:** Phases 3–6.
- **Target files:** `controllers/mustgather/template_test.go`; `controllers/mustgather/mustgather_controller_test.go`; `examples/`; `test/e2e/` (if applicable).
- **Required capabilities:** tests, examples-docs.
- **Verification hooks:** `make go-test`; `make test-e2e` (cluster); map to specs SC-001–SC-008.

## 6. Verification matrix (maps to spec acceptance)

| Category | Coverage | Files / Suites |
|----------|----------|----------------|
| Unit | Obfuscation Job template branches (chown, env, omit gather, PVC/ConfigMap mounts); `outputSubPath` **uniqueness**; multi-container **mount consistency**; CLI obfuscate with fixture data | `controllers/mustgather/template_test.go`, new obfuscate unit tests |
| Integration | `runObfuscate` processes sample bundle; output dir untouched on input; `obfuscation.log` present | UNVERIFIED package test dir |
| E2E | Mode 1 gather+obfuscate+upload SFTP verification; Mode 2/3 source PVC paths; negative invalid ConfigMap | `test/e2e/` (extend or new scenarios) |
| Manual / Cluster | Verify uploaded bundle has `x-ipv4-*` tokens, no Secret YAML, audit log present; permission denied if chown omitted | `oc logs` upload container; download SFTP bundle |
| Env var **validation** | **`DEFAULT_MUST_GATHER_IMAGE`** and **`OPERATOR_IMAGE`** **fail-fast** when empty/missing at operator start / Job creation; obfuscation env only injected when enabled | `main.go`, `mustgather_controller.go` — `make go-test` |
| N/A | Obfuscation progress in CR status | Deferred (specs A-002) |

**Spec mapping highlights:**

| Spec ID | Verification |
|---------|--------------|
| FR-001–FR-004, SC-001, SC-005 | Phases 3–4, E2E Mode 1/2/3 |
| FR-005–FR-006, SC-002–SC-003 | CLI + E2E bundle inspection |
| FR-007–FR-008, SC-004 | ConfigMap custom policy tests |
| FR-009, SC-006 | obfuscation.log in output |
| FR-011–FR-013, SC-007 | CEL + backward-compat tests |
| FR-014, SC-008 | Failed Job + condition types |

## 7. Risks, migrations, and operational follow-ups

- **Upgrade/migration:** Optional field — existing CRs unchanged (SC-007). Downgrade ignores unknown fields if CRD retained.
- **Compatibility (OpenShift/MicroShift/Hypershift):** No platform-specific changes (specs A-003); infra node affinity remains (repo-assessment §11.1 — no change to worker-only scheduling in this plan).
- **Permission gap (gather root / upload 65534):** Mitigate with gather chown; verify in unit and manual tests.
- **must-gather-clean fatal exits:** Job fails hard; ensure FR-014 (no partial upload).
- **Staging disk pressure:** Large bundles need emptyDir capacity on upload volume.
- **PVC concurrent access:** Enforce **per-invocation** **`directoryName` subPath** for writes; read-only source mounts; document administrator cleanup of old PVC subdirs.
- **Obfuscate-only retrieval:** emptyDir staging lost when pod exits — document operational procedure or defer Mode 2 without upload to post-Tech Preview (specs A-005).
- **Upstream API drift:** Pin must-gather-clean version in go.mod; vendor.

## 8. Open questions / SME decisions

| # | Question | Owner | Default if unresolved |
|---|----------|-------|------------------------|
| 1 | Source PVC namespace: operator namespace vs CR namespace (proposal vs existing `storage.claim` pattern) | SME / product | Use **CR namespace** for source claim to match existing `PersistentVolumeClaimReference` semantics |
| 2 | Obfuscate-only without upload: support in Tech Preview or reject via CEL | SME | **Reject** via CEL (FR-012) unless durable output sink added — aligns with staging emptyDir limitation |
| 3 | Distinct condition types vs extending `ReconcileError` only | Constitution / controller | Add **`ObfuscationFailed`** and **`ObfuscationConfigInvalid`** per constitution II |
| 4 | must-gather-clean worker count configurability | Performance SME | Hardcode 4 workers initially; `automaxprocs` for CPU limits (proposal) |
| 5 | E2E SFTP coverage for obfuscated bundles | Testing SME | Manual cluster verification acceptable for Tech Preview if CI lacks SFTP |

None beyond the above — implementation may proceed with listed defaults.
