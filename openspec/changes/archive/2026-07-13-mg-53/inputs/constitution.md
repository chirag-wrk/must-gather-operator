# must-gather-operator Constitution

**AgentRoutingMode:** PROVISIONAL

**Version:** 1.0.0 | **Ratified:** 2026-07-13 | **Last Amended:** 2026-07-13

## Core Principles

### I. Controller-Runtime Reconciler Patterns

Follow the existing `MustGatherReconciler` pattern: embed `util.ReconcilerBase`, use predicates to avoid status-update loops, and patch status via `ManageSuccess`/`ManageError`.

**Evidence:** `controllers/mustgather/mustgather_controller.go`, `controllers/mustgather/predicates.go`

### II. Generated Code Discipline

After API type changes, regenerate deepcopy, OpenAPI, and CRD manifests. Never hand-edit `zz_generated.deepcopy.go` or CRD OpenAPI sections.

**Evidence:** `Makefile` → `make generate`, `make manifests`; `boilerplate/openshift/golang-osd-operator/standard.mk`

### III. Verification Gates Before Merge

Run `make validate`, `make lint`, and `make go-test` before pushing. API changes require committed codegen output matching `make generate-check`.

**Evidence:** `boilerplate/openshift/golang-osd-operator/standard.mk` — `default: go-check go-test go-build`, `validate: generate-check`

### IV. In-Package Unit Testing

Controller unit tests use `package mustgather` (not `_test`), stdlib `testing`, table-driven patterns, and `t.Setenv` for environment overrides.

**Evidence:** `controllers/mustgather/template_test.go`

### V. Two-Container Job Model

Must-gather Jobs use `gather` and optionally `upload` containers with stable names. Upload is conditional on upload configuration. Shared volumes must stay consistent across containers that mount them.

**Evidence:** `controllers/mustgather/template.go` — container names, volume mounts

### VI. RBAC via Kubebuilder Markers

Declare RBAC with `//+kubebuilder:rbac` markers in controller source; regenerate deploy RBAC via `make manifests`. Do not edit generated RBAC YAML by hand.

**Evidence:** `controllers/mustgather/mustgather_controller.go` RBAC comments

### VII. FIPS Compliance

Build with `FIPS_ENABLED=true` (default). Do not remove FIPS build tags without compliance review.

**Evidence:** root `Makefile`, `fips.go` with `fips_enabled` build tag

### VIII. Commit and PR Conventions

Commit messages use `MG-NNN: <description>`. Run full `make` before PR. Type changes require `make generate && make manifests` in the commit.

**Evidence:** `openspec/inputs/agents.md` PR Expectations

## Additional Constraints

- **API group:** `operator.openshift.io/v1alpha1` — **Evidence:** `api/v1alpha1/groupversion_info.go`
- **CEL validation:** Use field-level XValidation with `has()` guards for omitempty union members — **Evidence:** `docs/api-contracts-guidelines.md` (referenced in agents.md)
- **Spec immutability:** MustGather spec is immutable; no spec-update reconcile logic required — **Evidence:** agents.md Architecture section
- **E2E build tag:** E2E tests use `//go:build osde2e` or `e2e`; excluded from default `go-test` — **Evidence:** `test/e2e/must_gather_operator_tests.go`, `standard.mk` TESTTARGETS filter

## Development Workflow

| Activity | Requirement | Evidence |
|----------|-------------|----------|
| Local unit tests | `make go-test` | `standard.mk` |
| Full preflight | `make` | `standard.mk` default target |
| Codegen refresh | `make generate && make manifests` after API edits | agents.md pitfall #1 |
| Verify committed codegen | `make validate` | `standard.mk` |
| Lint | `make lint` | `standard.mk` |

## Agent Routing

**AgentRoutingMode: PROVISIONAL** — no formal AGENTS.md agent IDs in target repo; use provisional taxonomy:

| Agent ID | Scope | When to route |
|----------|-------|---------------|
| API | CRD types, CEL, codegen | API/schema changes |
| OperatorController | Reconcile, Job template, upload wiring | Controller logic |
| Testing | Unit + e2e tests | Test additions |
| ManifestsBindata | CRD bundle, examples, deploy YAML | Manifest/example updates |

## Governance

- This constitution supersedes ad-hoc conventions for downstream Planning, Task Creation, and Code Generation agents.
- **Companion docs:** `openspec/inputs/agents.md` provides architecture context; repo-assessment.md provides file-level facts.
- **Conflicts:** If spec contradicts constitution, escalate in plan.md §8.
- **Amendments:** Bump Version and Last Amended when repo conventions change materially.
