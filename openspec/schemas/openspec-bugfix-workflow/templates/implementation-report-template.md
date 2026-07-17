# Implementation Report

**Change**: [CHANGE_NAME]
**Jira**: [JIRA_KEY]
**Completed**: [DATE]

## Summary

[One paragraph overview of what was implemented, test outcomes, and PR status.]

<!-- [ai-helpers mode — codegen_mode: ai-helpers] -->

[Include aggregate code eval results.]

<!-- [END mode-specific] -->

## Per-Task Reports

<!-- [ai-helpers mode — codegen_mode: ai-helpers] -->

| Task ID | Title | Phase | OAPE Command | Code Eval | Tests | Report |
|---------|-------|-------|--------------|-----------|-------|--------|
| T1_1 | … | Phase 1 | api-generate | 100% (3/3) | PASSED | [task-reports/T1_1.md](implementation/task-reports/T1_1.md) |

<!-- [direct mode — codegen_mode: direct] -->

| Task ID | Title | Phase | Tests | Report |
|---------|-------|-------|-------|--------|
| T1_1 | … | Phase 1 | PASSED | [task-reports/T1_1.md](implementation/task-reports/T1_1.md) |

<!-- [END mode-specific] -->

## Phases Completed

<!-- [ai-helpers mode — codegen_mode: ai-helpers] -->

| Phase | Tasks | OAPE Commands | Files Changed | Code Eval (avg) | Tests | Deviations |
|-------|-------|---------------|---------------|-----------------|-------|------------|
| [Phase 1] | T1_1, T1_2 | api-generate, api-implement | [count] | [N]% | PASSED | None |

<!-- [direct mode — codegen_mode: direct] -->

| Phase | Tasks | Files Changed | Tests | Deviations |
|-------|-------|---------------|-------|------------|
| [Phase 1] | T1_1, T1_2 | [count] | PASSED | None |

<!-- [END mode-specific] -->

## All Files Changed

### [Phase 1]

- `relative/path/to/file` — [task T1_1 — brief purpose]

<!-- [ai-helpers mode — codegen_mode: ai-helpers] -->

## Code Generation Eval Summary

| Task | Score | Cases pass | Refinement rounds |
|------|-------|------------|-------------------|
| T1_1 | 100% | 3/3 | 0 |

<!-- [END mode-specific] -->

## Test Results Summary

[Aggregate pass/fail/skip counts and notable failures.]

## Traceability Matrix

Map every file touched during implementation to the requirement IDs from specs.md.

| File | Task ID | Requirement IDs | Reason |
|------|---------|-----------------|--------|
| `relative/path/to/file` | T1_1 | FR-01, SC-01 | [brief reason] |

## Deviations Observed

[None — or link to deviation-observed.md and summarize deviation count.]

## Draft Pull Request

| Field | Value |
|-------|-------|
| Fork | [fork_repo_url] |
| Branch | [feature branch name] |
| PR URL | [draft PR URL] |
