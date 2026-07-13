# Implementation Design Bundle

**Change:** mg-53  
**Jira:** MG-53  
**Phase:** Phase 1 — API schema  
**Current Task:** T1_1  
**Task Title:** Define uploadTarget API types and CEL validation

**Codegen mode:** `direct` (from `openspec/config.yaml`)  
**Working folder:** `/home/cdate/must-gather-oper/must-gather-operator` (`use_working_folder_as_repo: true`)  
**Assigned agent:** API_Agent (provisional)

This bundle replaces an OpenShift Enhancement Proposal (EP) when driving implementation from `/opsx:apply`. It is composed from approved OpenSpec artifacts and scoped to the **current task only** (one Task ID per invocation).

---

## Input precedence (conflicts)

1. constitution.md (non-negotiable guardrails)
2. specs.md (requirements and acceptance criteria)
3. plan.md (architectural context and verification hooks)
4. repo-assessment.md (target files, Makefile targets, evidence)
5. tasks.md §4 payload for the **current Task ID** (most specific)

---

## Constitution (guardrails)

Relevant to T1_1:

- **Principle II — Generated Code Discipline:** After API type changes, run `make generate && make manifests`. Never hand-edit `zz_generated.deepcopy.go` (T2_1 handles regeneration; T1_1 edits types only).
- **Principle III — Verification Gates:** API changes require committed codegen matching `make generate-check` before merge (downstream T2_1/T7_2).
- **API group:** `operator.openshift.io/v1alpha1` — do not change group/version.
- **CEL validation:** Field-level XValidation with `has()` guards for omitempty union members (constitution Additional Constraints).
- **Commit convention:** `MG-53: <description>` when committing.

---

## Specifications (requirements)

T1_1 satisfies at the type level:

| Requirement | T1_1 contribution |
|-------------|-------------------|
| **FR-001** | SFTP upload destination type via `uploadTarget.type` |
| **FR-004** | CEL rejects SFTP type without `sftp` block; rejects `sftp` block without SFTP type |
| **FR-007** | Group upload settings under `spec.uploadTarget` discriminated union |
| **FR-008** | Remove legacy top-level `caseID`, `caseManagementAccountSecretRef`, `internalUser` |
| **FR-010** | Restrict `type` enum to SFTP only |
| **SC-003** | Admission-time rejection via CEL (validated in T2_2, T5_6) |

User stories traced: User Story 2 (structured upload destination), User Story 5 (auditable upload block).

---

## Plan (architectural context)

**Phase 1 goal:** Define `UploadTarget`, `SFTPUploadTargetConfig`, CEL validation; remove legacy top-level upload fields.

**Target file:** `api/v1alpha1/mustgather_types.go`

**API shape (from plan §3.1):**

```yaml
spec:
  uploadTarget:
    type: SFTP
    sftp:
      caseID: "12345678"
      caseManagementAccountSecretRef:
        name: case-management-account
      host: sftp.access.stage.redhat.com   # optional; default sftp.access.redhat.com
      internalUser: true                    # optional; default true
```

**Verification hooks (Phase 1):** CEL rule review; types compile; `make generate` succeeds (T2_1).

---

## Repo assessment (grounding)

| Fact | Evidence |
|------|----------|
| Current spec has flat required `caseID`, `caseManagementAccountSecretRef`, `internalUser` | `api/v1alpha1/mustgather_types.go:29–61` |
| No `uploadTarget` in committed types | repo-assessment §0 GREENFIELD |
| Stale uncommitted `UploadTarget` in deepcopy | Working tree — reconcile via T2_1 `make generate` |
| CEL guidelines | `docs/api-contracts-guidelines.md` (referenced in agents.md) |
| Codegen commands | `make generate`, `make manifests` (Makefile / standard.mk) |

**Forbidden edits (T1_1):** `zz_generated.deepcopy.go`, unrelated spec fields (audit, proxy, timeout).

---

## Task payload (current task)

### Task T1_1: Define uploadTarget API types and CEL validation

- **Objective:** Introduce `UploadTarget`, `SFTPUploadTargetConfig`, and remove legacy top-level upload fields per EP.
- **Target file(s):** `api/v1alpha1/mustgather_types.go`
- **Non-goals / forbidden edits:** Do not hand-edit `zz_generated.deepcopy.go`; do not change unrelated spec fields (audit, proxy, timeout).
- **Implementation notes:** Use kubebuilder union markers and field-level CEL with `has()` guards for omitempty members. Default `host` to `sftp.access.redhat.com`. API group remains `operator.openshift.io/v1alpha1`.
- **Acceptance criteria:** Types compile; CEL rule enforces SFTP type requires `sftp` block; FR-001, FR-004, FR-007, FR-008, FR-010 satisfied at type level.
- **Downstream handoff:** Frozen API shape for T2_1 codegen.

---

## API specification (derived — for oape:api-generate)

- **Group:** `operator.openshift.io`
- **Version:** `v1alpha1`
- **Kind:** `MustGather`
- **Scope:** Namespaced
- **FeatureGate:** N/A

### New types

**UploadTargetType** (string enum):
- `SFTP` — only supported value in MG-53 (FR-010)

**UploadTarget** (discriminated union on `MustGatherSpec`):
- `type` (UploadTargetType, required): destination discriminator
- `sftp` (SFTPUploadTargetConfig, optional): SFTP-specific settings; required when `type == SFTP` (CEL)

**SFTPUploadTargetConfig**:
- `caseID` (string, required when SFTP): Red Hat case identifier (FR-003)
- `caseManagementAccountSecretRef` (LocalObjectReference, required when SFTP): credential secret (FR-003)
- `host` (string, optional): SFTP server hostname
  - Validation: DNS hostname format (kubebuilder Pattern if applicable)
  - Default: `sftp.access.redhat.com` via `+kubebuilder:default` (FR-002, A-007)
  - Immutable: N/A (spec immutability applies at CR level)
- `internalUser` (bool, optional): internal-user upload mode
  - Default: `true` (FR-005)

### Removed fields (breaking — FR-008)

Remove from `MustGatherSpec`:
- `caseID`
- `caseManagementAccountSecretRef`
- `internalUser`

### CEL validation (field-level XValidation on UploadTarget)

1. When `type == "SFTP"`, `has(self.sftp) && self.sftp != null` (FR-004)
2. When `type != "SFTP"`, forbid unsupported types (only SFTP enum value allowed — FR-010)
3. Use `has()` before accessing omitempty `sftp` member

### Status fields

No status changes in T1_1. Distinct upload condition types added in T7_1.

---

## Reconciliation workflow (derived — for oape:api-implement)

**Not in T1_1 scope.** Controller changes deferred to T4_1/T4_2/T7_1.

Forward reference for downstream tasks:

1. Validate spec (CEL at admission — T1_1)
2. When `uploadTarget` nil → gather-only Job (T4_1, FR-006)
3. When `uploadTarget.type == SFTP` → two-container Job with env wiring (T4_1)
4. Skip secret copy when upload disabled (T4_2)
5. Set distinct status conditions on failure (T7_1)

### Multi-container mount consistency (T4_1 / T5_3)

The **gather container** and **upload container** share `must-gather-output` volume. Any mount change MUST apply to **both** containers. Hostname is env-only — must not alter volume subPath.

### Status conditions (T7_1)

| Condition type | Failure category | Distinct from generic? |
|----------------|------------------|------------------------|
| `UploadConfigurationInvalid` | CR uploadTarget shape invalid at runtime | Yes |
| `UploadCredentialsInvalid` | Secret missing or malformed | Yes |
| `UploadOperatorConfigInvalid` | Empty `OPERATOR_IMAGE` / `DEFAULT_MUST_GATHER_IMAGE` | Yes |
| `UploadJobFailed` | Upload container Job failure | Yes |

Do not use single `ReconcileError` for all upload failures.

---

## Edge-Case & Input Validation Coverage

User-facing hostname field (`uploadTarget.sftp.host`) — edge case handling across MG-53:

| Edge case | Empty | Whitespace | Separator-only | Trim | Handled by | Test file |
|-----------|-------|------------|----------------|------|------------|-----------|
| `host` omitted | default `sftp.access.redhat.com` | N/A | N/A | N/A | T1_1 kubebuilder default | T5_2 |
| `host` empty string | fallback to default | N/A | N/A | trim/reject | T4_1 controller | T5_2 **test** |
| `host` whitespace-only | reject or trim | handled | N/A | handled | T4_1 controller | T5_2 **test** |
| `host` valid staging | N/A | N/A | N/A | N/A | T3_1/T4_1 env wiring | T5_2 **test** |

T1_1 establishes API default; runtime empty/whitespace/trim validation and **test** coverage in T5_2.

---

## Environment variable default validation

| Environment variable | Default / source | Empty guard | Task |
|---------------------|------------------|-------------|------|
| `SFTP_HOST` (upload container) | `uploadTarget.sftp.host` or `sftp.access.redhat.com` | Controller sets from CR; script defaults if unset | T3_1, T4_1 |
| `OPERATOR_IMAGE` (operator deployment) | Operator CSV/env | Fail-fast with `UploadOperatorConfigInvalid` when empty for upload-enabled CR | T7_1 |
| `DEFAULT_MUST_GATHER_IMAGE` | Operator env | Fail-fast with `UploadOperatorConfigInvalid` when empty | T7_1 |

T1_1 defines CR-level default for `host`. Operator **environment variable** empty **validation** and **test** results in T7_1/T5_4.

---

## Verification (this task)

| Hook | Command / test | Task ID |
|------|----------------|---------|
| Compile | `go build ./api/...` | T1_1 |
| Type check | Review CEL markers and union structure | T1_1 |
| Codegen (downstream) | `make generate && make manifests` | T2_1 |
| CEL admission | CRD dry-run invalid configs | T2_2, T5_6 |

**T1_1 acceptance criteria:**
- Types compile
- CEL enforces SFTP type requires `sftp` block
- Legacy top-level upload fields removed
- FR-001, FR-004, FR-007, FR-008, FR-010 satisfied at type level

---

## Revision feedback (when re-running after task rejection)

None — first invocation.

---

## Implementation backlog summary

| Metric | Value |
|--------|-------|
| Total tasks | 16 |
| Current | T1_1 (1/16) |
| Critical path | T1_1 → T2_1 → T4_1 → T4_2 → T7_1 → T7_2 |
| Next after T1_1 | T2_1 (codegen), T3_1 (parallel after T1_1) |

Run `/opsx:apply` to execute T1_1 in direct mode.
