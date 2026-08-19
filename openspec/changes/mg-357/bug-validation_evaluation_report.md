# Evaluation Report: bug-validation

**Change:** mg-357  
**Artifact:** bug-validation (`bug-validation.json`)  
**Evaluated at:** 2026-08-19T06:47:43Z

## Eval Summary

| Metric | Value |
|--------|-------|
| Overall score | 95% |
| Cases passed | 5 / 5 |
| Cases failed | 0 |
| Refinement applied | No |

## Rubric Checks

| Check ID | Score | Pass | Notes |
|----------|-------|------|-------|
| valid-json-schema | 100 | Yes | All required keys present per bug-validation-template.md |
| scoring-math | 100 | Yes | `round(0.6×78 + 0.4×90) = 83` matches `overall_score` |
| status-gate-logic | 100 | Yes | PASS: score ≥ 80, no blockers, has_steps_to_reproduce=true, has_error_evidence=true |
| no-fabrication | 100 | Yes | Findings grounded in jira-spec.md text only |
| input-grounding | 75 | Yes | Correctly identifies epic, code paths, and gaps |

## Gap Analysis

### Input artifacts reviewed

- `inputs/jira.yaml` — MG-357 metadata, epic MG-297
- `inputs/jira-spec.md` — full ticket description and acceptance criteria
- `AGENTS.md` — no Validation Stage Hints section; `project_ecosystem` correctly omitted

| Gap | Source | Severity |
|-----|--------|----------|
| Severity/priority not classified in ticket | jira-spec.md | MODERATE |
| No concrete failing CR YAML in repro steps | jira-spec.md | MINOR |
| No sample log excerpts | jira-spec.md | MINOR |
| No OpenShift/operator version in environment | jira-spec.md | MINOR |

### Consistency

Validation scores align with ticket quality: strong root-cause narrative and acceptance criteria, weaker on environment metadata and log evidence.

### Grounding

All `quality_issues` and `missing_elements` quote or paraphrase ticket content. No invented failure modes.

## Quality Assessment

- **Completeness:** Validation JSON covers all required schema fields.
- **Consistency:** PASS status matches rubric threshold and absence of blockers.
- **Grounding:** Scores reflect actual ticket strengths (detailed root cause, epic link, acceptance criteria) and weaknesses (no severity, limited env details).
- **Agent routing:** AGENTS.md has no Validation Stage Hints; ecosystem extension correctly omitted.

## Recommendations

- During **bug-report** stage: extract ARD from MG-297 epic PRs; include concrete MustGather CR example for gather-failure repro.
- During **repro-verification**: capture upload-container logs showing pgrep → upload sequence on failed gather.
- Verify whether gather exit-code masking and upload wait logic should be fixed in one change or split.
