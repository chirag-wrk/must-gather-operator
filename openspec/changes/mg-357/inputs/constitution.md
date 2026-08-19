# Must-Gather Operator Constitution

**AgentRoutingMode:** PROVIDED (component-based routing per AGENTS.md — no formal agent IDs defined)

**Version**: 1.0 | **Ratified**: 2026-08-19 | **Last Amended**: 2026-08-19

## Core Principles

### I. Controller-Runtime Reconciliation Patterns
Follow controller-runtime patterns: single reconciler, predicates for event filtering, finalizers for cleanup, status updates via `Status().Update()`.

**Evidence:** `controllers/mustgather/mustgather_controller.go`, `controllers/mustgather/predicates.go`

### II. Job Template as Operand Definition
Gather/upload Job specs are built in `template.go` — do not hand-edit generated CRD/OpenAPI files; run `make generate` + `make manifests`.

**Evidence:** `controllers/mustgather/template.go`, `AGENTS.md` Critical Patterns

### III. Test-First Verification Gates
All changes must pass `make` (build + test + lint). Unit tests use fake client + interceptClient; E2E uses Ginkgo (`make test-e2e`).

**Evidence:** `AGENTS.md` Quick Reference, `harness-evals/harness-docs/MGO_TESTING.md`

### IV. Minimal Scope Bug Fixes
Bug fixes touch only files required by root cause. No API/CRD changes unless unavoidable. Preserve `obfuscate.source` direct-upload behavior.

**Evidence:** MG-357 acceptance criteria; `uploadCommandDirect` in `template.go`

### V. No Secret Replication
Secrets referenced via SecretKeyRef from CR namespace; only trusted CA ConfigMaps replicated.

**Evidence:** `AGENTS.md` Critical Patterns

## Development Workflow

- Build/test/lint: `make` or `make go-test`
- Generate: `make generate`, `make manifests`
- FIPS enabled by default
- Working-folder mode active for MG-357 (`inputs/jira.yaml`)

## Governance

Supersedes generic patterns when conflicts arise. AGENTS.md and CLAUDE.md provide architecture context. Implementation follows approved `tasks.md` with per-task approval via `/opsx-apply`.
