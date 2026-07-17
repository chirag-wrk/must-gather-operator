# Bug Fix Design Bundle

**Change:** [CHANGE_NAME]
**Jira:** [JIRA_KEY]
**Current Task:** [TASK_ID — e.g. T1_1]
**Task Title:** [TASK_TITLE]

This bundle drives OAPE commands from `/opsx:apply` for bug fix implementation.
It is composed from approved OpenSpec bug fix artifacts and scoped to the
**current task only** (one Task ID per OAPE invocation).

---

## Input precedence (conflicts)

1. constitution.md (non-negotiable guardrails)
2. rca-report.md (root cause analysis — most critical for bug fix)
3. bugfix-plan.md (fix approach, target files, verification)
4. bug-report.md (bug details, ARD context, original PR references)
5. repro-verification-report.md (reproduction evidence, failure signature)
6. tasks.md §4 payload for the **current Task ID** (most specific)

---

## Constitution (guardrails)

<!-- Paste or summarize constitution.md sections relevant to this fix -->

---

## Root Cause Analysis (RCA)

<!-- Paste or summarize rca-report.md: root cause statement, affected components, fix area -->

---

## Bug Fix Plan (fix approach)

<!-- Paste or summarize bugfix-plan.md: fix strategy, target files, verification matrix -->

---

## Bug Report (context)

<!-- Paste or summarize bug-report.md: bug description, steps to reproduce, ARD context, original PR diffs -->

---

## Repro Verification (evidence)

<!-- Paste or summarize repro-verification-report.md: failure signature, logs captured -->

---

## Task payload (current task)

<!-- Paste tasks.md §4 subsection for the current Task ID only -->

---

## API specification (derived — for oape:api-generate)

- **Group:**
- **Version:**
- **Kind:**
- **Scope:** Cluster | Namespaced
- **FeatureGate:** (if applicable)

### Spec fields

- `fieldName` (type): description
  - Validation:
  - Default:
  - Immutable:

### Status fields

- `conditions`: Standard OpenShift conditions
- `observedGeneration`: int64

---

## Reconciliation workflow (derived — for oape:api-implement)

1. Validate spec
2. …

### Dependent resources

- ConfigMap: …
- Deployment: …

### Status conditions

- Available: …
- Progressing: …
- Degraded: …

### Events

- …

### Cleanup / finalizers

- …

---

## Verification (this task)

<!-- From current task Acceptance criteria and bugfix-plan.md §7 verification matrix -->

| Hook | Command / test | Task ID |
|------|----------------|---------|
| Unit | make test | [TASK_ID] |
| Regression | … | … |
| Repro verify | … | … |

---

## Revision feedback (when re-running after task rejection)

<!-- User feedback from prior task gate rejection — omit when none -->
<!-- Re-run the current task only; compose bundle for that Task ID -->
