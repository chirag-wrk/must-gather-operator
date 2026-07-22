<!-- Companion artifact: repo-assessment.md (target files, reusable assets, risks) -->
# must-gather-operator Constitution

**AgentRoutingMode:** PROVIDED
<!-- openspec/inputs/agents.md exists and was parsed. NOTE (see Governance): several of its
     architectural claims were found NOT to hold on the pinned branch/commit during
     repo-assessment.md's evidence-gathering pass. AgentRoutingMode is still PROVIDED because
     the file exists and its Domain Guideline Index / Quick Reference / Code Conventions /
     Common Pitfalls sections are used as routing hints below, but downstream agents MUST
     defer to repo-assessment.md wherever the two sources conflict on architecture or file
     existence. -->

**Version**: 1.0.0 | **Ratified**: 2026-07-14 | **Last Amended**: 2026-07-14

## Core Principles

### I. Reconcile Through `operator-utils.ReconcilerBase`, Not Raw `client.Client`
`MustGatherReconciler` embeds `util.ReconcilerBase` (github.com/redhat-cop/operator-utils) rather than wiring a bare controller-runtime `client.Client`. All resource lifecycle operations MUST go through its helpers — `CreateResourceIfNotExists` (which sets `controllerutil.SetControllerReference` automatically), `DeleteResourceIfExists`, `ManageError`, `ManageSuccess` — rather than calling `r.GetClient().Create/Delete` directly for owned resources. New code that bypasses this base loses automatic owner-reference wiring and the shared status-condition update path.

**Evidence:** `controllers/mustgather/mustgather_controller.go:55-59` (struct embedding); `vendor/github.com/redhat-cop/operator-utils/pkg/util/reconciler.go:215-222` (owner-ref wiring inside `CreateResourceIfNotExists`).

### II. Status Is Reported Exclusively via `metav1.Condition` Through `ManageError`/`ManageSuccess`
`MustGather` implements `apis.ConditionsAware` (`GetConditions`/`SetConditions`) and all status changes flow through `ManageError`/`ManageSuccess`, which set the `ReconcileError`/`ReconcileSuccess` condition types. The legacy `Status.Status` and `Status.Reason` string fields exist in the type but are never written by the controller — they are dead fields. New status semantics (e.g., an upload-specific failure condition) MUST add a new, more specific `Condition` type through the same `ManageError`/`ManageSuccess` path, not a new status subsystem, and MUST NOT resurrect the dead `Status`/`Reason` string fields.

**Evidence:** `api/v1alpha1/mustgather_types.go:79-94`; `mustgather_controller.go` calls `r.ManageError(...)`/`r.ManageSuccess(...)` exclusively (no direct `.Status().Update()` calls).

### III. Two-Container Job Model with a Shared `EmptyDir` Volume — Mount Changes Apply to Every Container That Needs the Data
Every `MustGather` Job runs `gather` and (today, unconditionally) `upload` sharing a process namespace and the same `must-gather-output` `EmptyDir`, mounted at the identical path in both containers via the shared `outputVolumeName`/`volumeMountPath` constants. There is no PVC anywhere in this codebase — volumes are ephemeral and scoped to a single Job's lifetime. Any change to the shared mount path, or any new shared volume, MUST update every container function that mounts it (`getGatherContainer`, `getUploadContainer`), reusing the existing constants rather than introducing new literals.

**Evidence:** `controllers/mustgather/template.go:21-24` (constants), `:164-169` and `:194-198` (both containers' `VolumeMounts`), `:131-140` (`EmptyDirVolumeSource`, no PVC).

### IV. Generated Code and Manifests Are Never Hand-Edited — Three Places Must Regenerate Together
`api/v1alpha1/zz_generated.deepcopy.go` and `zz_generated.openapi.go` are `controller-gen` output (`make generate`). The CRD schema exists in **three** committed copies that must be regenerated together and never hand-edited: `deploy/crds/operator.openshift.io_mustgathers.yaml`, `bundle/manifests/tech-preview/operator.openshift.io_mustgathers.yaml`, and the CSV template outputs under `bundle/manifests/tech-preview/`. `boilerplate/` itself carries an explicit "DO NOT EDIT" header and is refreshed only via `make boilerplate-update`.

**Evidence:** `deploy/crds/operator.openshift.io_mustgathers.yaml` header (`controller-gen.kubebuilder.io/version: v0.16.4`); `bundle/manifests/tech-preview/operator.openshift.io_mustgathers.yaml` (duplicate schema); `boilerplate/generated-includes.mk` header comment.

### V. Cluster-Wide RBAC, Namespace-Scoped Reconciliation
The `must-gather-operator` `ClusterRole` grants cluster-wide verbs on `mustgathers`/`jobs`/`secrets`/etc., but `main.go`'s `ctrl.Manager` is constructed with `Cache.DefaultNamespaces` restricted to the single `must-gather-operator` namespace. In practice, `MustGather` CRs are only reconciled when created in that namespace — this is the actual deployment model (confirmed by `README.md`'s install instructions: `oc new-project must-gather-operator` then apply the CR there), not a cluster-wide, any-namespace CR pattern. Case-management Secrets are read from the CR's namespace and copied into the operator namespace before the Job mounts them (never mounted cross-namespace directly) — new secret-consuming code MUST follow this same copy-then-mount pattern, not attempt a cross-namespace volume/secret reference.

**Evidence:** `main.go:59-60,109-123` (namespace-scoped cache); `deploy/02_must-gather-operator.ClusterRole.yaml` (cluster-wide RBAC); `mustgather_controller.go:208-243` (Secret copy pattern).

### VI. In-Package Table-Driven Unit Tests; `osde2e`-Tagged Ginkgo E2E for Install Verification Only
Unit tests live beside their source as `package mustgather` (not `_test` suffix package) using plain `testing` in table-driven form (`template_test.go`, `mustgather_controller_test.go`), giving access to unexported symbols and package constants. E2E tests are gated by the `osde2e` build tag (not `e2e`) and use Ginkgo/Gomega plus `openshift/osde2e-common` client/matchers; today they verify only that install-time artifacts (namespace, roles, rolebindings, clusterroles) exist — there is no CR-level (SFTP/upload) e2e coverage yet. New unit tests MUST follow the in-package table-driven pattern; there is no `interceptClient`-style failure-injection helper in this repo today — use controller-runtime v0.21's native `fake.Client` `WithInterceptorFuncs` if failure injection is needed, do not invent a bespoke wrapper without checking for this native option first.

**Evidence:** `controllers/mustgather/template_test.go:1-16` (package/import style); `test/e2e/must_gather_operator_tests.go:1-20` (`//go:build osde2e`, Ginkgo/osde2e-common imports); zero repo matches for `interceptClient`.

### VII. FIPS-Enabled Build Is Non-Negotiable
`Makefile` sets `FIPS_ENABLED=true` and `fips.go` (build tag `fips_enabled`) imports `crypto/tls/fipsonly`. Any new dependency or code path introducing TLS/crypto usage (e.g., the SFTP host-configurability work) MUST NOT bypass this — no new library should perform its own TLS handshake outside the FIPS-constrained path already used by the vendored `sftp`/`ssh` client tooling invoked from `build/bin/upload`.

**Evidence:** `Makefile:1`; `fips.go:1-13`.

## Additional Constraints

- **API group / scope:** `operator.openshift.io/v1alpha1`, `MustGather` is `Namespaced`-scoped (not cluster-scoped) — **Evidence:** `deploy/crds/operator.openshift.io_mustgathers.yaml` (`spec.group`, `spec.scope`).
- **No CEL/webhook validation exists yet:** despite `ep.md`'s proposal to add a `+kubebuilder:validation:XValidation` union rule for `UploadTarget`, there is currently **zero** `XValidation` marker anywhere in `api/v1alpha1/` — this will be the first CEL rule in the codebase, authored fresh, not copied from an existing in-repo pattern — **Evidence:** full read of `mustgather_types.go`, no `XValidation` string present.
- **Env var default-image guard asymmetry:** `DEFAULT_MUST_GATHER_IMAGE` is read via unguarded `os.Getenv` (empty value silently accepted); `OPERATOR_IMAGE` is read via guarded `os.LookupEnv` (missing value returns an error to `ManageError`). New required environment variables MUST use the guarded (`OPERATOR_IMAGE`) pattern — **Evidence:** `controllers/mustgather/template.go:162` vs `controllers/mustgather/mustgather_controller.go:345-350`.
- **Module/vendoring:** Go 1.24.0, dependencies vendored under `vendor/` — new dependencies must be added via `go mod vendor` (implied by the presence of a populated `vendor/` tree), not left unvendored.
- **Commit convention:** commit messages are prefixed with the Jira ticket ID (`MG-NNN: <description>`) — **Evidence:** `git log` entries `MG-93: Removed hardcoded value of gather container image (#276)`, `MG-79: Corrected proxyConfig fields`.
- **Naming split (informational, not to be "fixed" incidentally):** the OLM package/CSV name (`support-log-gather-operator`) differs from the Go module/binary name (`must-gather-operator`) — pre-existing, unrelated to this feature — **Evidence:** `bundle/manifests/support-log-gather-operator.package.yaml` vs `go.mod` module path.

## Development Workflow

| Activity | Requirement | Evidence |
|----------|-------------|----------|
| Local unit tests | `make go-test` (or `make test`) | `boilerplate/openshift/golang-osd-operator/standard.mk:262-263,311-312` |
| Full local gate | `make` (default: `go-check go-test go-build`) | `standard.mk:134-135` |
| Lint | `make lint` (= `olm-deploy-yaml-validate go-check`, runs `golangci-lint`) | `standard.mk:307-308,175-179` |
| Codegen refresh after API type changes | `make generate` (= `op-generate go-generate openapi-generate manifests`), then `make manifests` regenerates **both** CRD YAML copies | `standard.mk:237-238,227-236` |
| Pre-PR verification | `make validate` (= `boilerplate-freeze-check generate-check`) | `standard.mk:303-304` |
| PR review/approval | Gated by `OWNERS` (reviewers/approvers/maintainers lists) via Prow | `OWNERS` |
| E2E (osde2e) | Requires live OpenShift cluster; built/pushed as separate image (`make e2e-binary-build`/`e2e-image-build-push`), not run via plain `go test` | `boilerplate/openshift/golang-osd-e2e/standard.mk:60-69` |

## Agent Routing

<!-- AgentRoutingMode: PROVIDED — summarized from openspec/inputs/agents.md's Domain Guideline
     Index and Quick Reference. Several referenced domain files (docs/*.md) and behaviors do
     NOT exist on the pinned branch (see repo-assessment.md §11.1) — downstream agents should
     treat the routing table below as "where to look if such logic is added", not as proof
     the logic already exists. -->

| Agent ID | Scope | When to route |
|----------|-------|---------------|
| api-types | `api/v1alpha1/` CRD types, deepcopy, OpenAPI generation | Any `MustGatherSpec` field add/remove/rename (this feature's `UploadTarget` work) |
| controller-reconcile | `controllers/mustgather/mustgather_controller.go` | Reconcile loop, finalizer/cleanup ordering, Secret copy logic |
| job-template | `controllers/mustgather/template.go` | Job/container spec construction, env vars, volume mounts |
| upload-script | `build/bin/upload` | Actual SFTP upload mechanics (host, auth, proxy) — runs inside the operator image, not a separate image |
| manifests-rbac | `deploy/`, `bundle/manifests/`, `bundle/manifests/tech-preview/` | RBAC changes, CRD regeneration, OLM bundle/CSV updates — always via `make manifests`, never hand-edited |
| examples-docs | `examples/*.yaml`, `README.md` | Sample CR updates, migration/documentation guidance (per specs.md FR-010) |
| tests | `controllers/mustgather/*_test.go` (unit), `test/e2e/*.go` (osde2e) | New table-driven unit cases; osde2e e2e additions |

*(agents.md's own referenced `docs/*-guidelines.md` domain files do not exist in this repo today — routed here as file-path scopes derived directly from repo-assessment.md's Target Files/Component Map instead.)*

## Governance

- This constitution supersedes ad-hoc conventions for downstream Planning, Task Creation, and Code Generation agents in this change.
- **Amendments:** require documented evidence of repo change; bump Version and Last Amended date.
- **Conflicts:** if `specs.md` contradicts a principle here, escalate in `plan.md` §8 — do not silently override.
- **Companion docs — precedence order:** (1) **this constitution** + `repo-assessment.md` (both freshly re-verified against the pinned commit `e766ff0`) take precedence over (2) `openspec/inputs/agents.md`, wherever the two conflict on file existence, architecture, or behavior. `agents.md`'s Domain Guideline Index, Quick Reference commands, and Code Conventions sections that were independently re-confirmed during repo-assessment (in-package table-driven unit tests; `MG-NNN:` commit prefix; `Owns()`+predicate watch pattern; generic helper `ToPtr[T any]`) remain trustworthy; its claims about `spec.uploadTarget`, `validation.go`, `constant.go`, `docs/*.md`, `interceptClient`, `test/library/`, cluster-wide CR watching, and `//go:build e2e` are **not** trustworthy for this branch and must not be assumed true by Planning/Tasks/Code-Generation agents without re-verification.
- **Complexity:** new patterns (e.g., the first `XValidation` CEL rule in this repo) must justify their design explicitly in `plan.md` rather than assuming an existing in-repo precedent, since none exists.
