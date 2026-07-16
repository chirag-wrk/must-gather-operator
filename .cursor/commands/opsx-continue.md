---
name: /opsx-continue
id: opsx-continue
category: Workflow
description: Continue agile-workflow change - create next artifact, eval gate, refine, approve (OPSX)
---

Continue working on a change by creating the **next** artifact (one per invocation), then **eval → refine artifact → user approval**.

**Input**: Optional change name after `/opsx-continue` (e.g. `/opsx-continue cm-830`).

## Schema package (resolve first existing path)

| Role | Installed | Distribution |
|------|-----------|--------------|
| Schema root | `openspec/schemas/openspec-agile-workflow/` |
| Stage gate | `{schema_root}/stage-gate/` | same |
| Stage evals | `{schema_root}/evals/<stage>_eval.yaml` | same |
| Templates | `{schema_root}/templates/` | same |

## Steps

1. Select change (`openspec list --json` if name not given).
2. `openspec status --change "<name>" --json`
3. Read `openspec/changes/<name>/inputs/jira.yaml` (required).
4. **Resolve repo target before repo-assessment** (see schema `target_repo` and `working_folder_repo`):
   - **Working-folder mode:** If the user directs using the working folder as the repo,
     set `use_working_folder_as_repo: true` in `inputs/jira.yaml`, record
     `working_folder_path`, analyze cwd — do not ask for GitHub URL or clone separately.
   - **Default mode:** If the next ready artifact is `repo-assessment` (or `constitution`) and
     `target_repo` is absent or empty in `jira.yaml`:
     - Ask the user once: "Provide the URL of the target GitHub repository
       (e.g. https://github.com/org/repo)."
     - Persist `target_repo` to `inputs/jira.yaml`.
     - Verify the repository is accessible before creating repo-assessment.
     - **Do not** create repo-assessment or constitution until `target_repo` is recorded.
   - For earlier artifacts (`validation`, `specs`), `target_repo` is not required.
5. Pick first artifact with `status: "ready"`.
5a. **Phase-iterative check** (ONLY when next ready artifact is `tasks`):
    - Read `config.yaml → flags.phase_iterative` (default: true).
    - If true:
      - Read `implementation/state.yaml` (or initialize if missing) to get
        `current_plan_phase` (default: 1) and `total_plan_phases`.
      - If `current_plan_phase > total_plan_phases`: all phases done — skip task
        generation; proceed to archive. STOP.
      - Read `plan.md` and extract Phase N details (goal, target files,
        dependencies, verification hooks).
      - Set `phase_scope: N` in generation context metadata.
      - If Phase N > 1: existing tasks.md contains prior phases marked `[x]` —
        new tasks will be appended.
5b. **Task sizing prompt** (ONLY when next ready artifact is `tasks`):
    - Read `config.yaml → flags.task_sizing`.
    - If `prompt_user` is true:
      - ASK the user ONCE:
        ```
        Phase {N} of plan.md: {phase_goal}.
        How many tasks for this phase?
        Enter a range: min max (e.g. "3 8")
        Press Enter to use defaults ({default_min}–{default_max}).
        ```
      - Parse response. Empty → defaults from config.
      - Inject into generation context as metadata field:
        `task_sizing: { min: X, max: Y, consolidation_threshold: Z }`
    - If `prompt_user` is false: inject defaults silently (no prompt).
    - **Do NOT re-prompt during eval gate, feedback loop, or regeneration.**
      The task_sizing metadata persists for the lifetime of this artifact generation.
6. **Telemetry — signal artifact start** (silent, non-blocking; starts phase clock on dashboard):
   ```bash
   python -m openspec.telemetry.auto on-artifact-start --change "<name>" --artifact "<artifact-id>" --phase <N>
   ```
   Omit `--phase` for non-task artifacts.
7. `openspec instructions <artifact-id> --change "<name>" --json` → create artifact at `outputPath` (**v1**).
   - Generation uses **`{schema_root}/templates/`** (from openspec instructions).
   - For `tasks` artifact: pass `phase_scope` and `task_sizing` metadata to the template.
   - If Phase N > 1: append new phase tasks to existing tasks.md.
8. **Telemetry — signal artifact written** (silent, non-blocking; emits `phase_progress` with partial tokens):
   ```bash
   python -m openspec.telemetry.auto on-artifact-created --change "<name>" --artifact "<artifact-id>" --phase <N>
   ```
9. **Stage eval gate** — read and follow **`{schema_root}/stage-gate/STAGE_EVAL_GATE_PROMPT.md`** Steps 1–5 exactly.
   This is the single source of truth for eval scoring, artifact refinement, evaluation report
   generation, and user approval. Key paths used by the prompt:
   - Artifact-to-eval mapping: `{schema_root}/stage-gate/artifact-eval-map.yaml`
   - Stage eval cases: `{schema_root}/evals/<stage>_eval.yaml`
   - Eval results output: `openspec/changes/<name>/eval-results/<artifact-id>.yaml`
   - Evaluation report output: `openspec/changes/<name>/eval-results/<artifact-id>_evaluation_report.md`
   - On user rejection: follow **`{schema_root}/stage-gate/USER_FEEDBACK_PROMPT.md`**
   - On `specs` rejection: **exit workflow** (schema `exit_on_reject.specs`) — do NOT regenerate; STOP
10. **Telemetry — signal waiting for approval** (silent, non-blocking; emits `phase_progress` with eval score):
    ```bash
    python -m openspec.telemetry.auto on-waiting-approval --change "<name>" --artifact "<artifact-id>" --phase <N> --score <eval_score>
    ```
11. **After user approves or rejects**, signal the outcome (finalizes phase metrics):
    ```bash
    python -m openspec.telemetry.auto on-artifact-complete --change "<name>" --artifact "<artifact-id>" --phase <N> --status passed --score <eval_score> --label "<quality_label>"
    ```
    Use `--status failed` if the user rejects the artifact.
    Omit `--phase` for non-task artifacts.

## Artifact order (openspec-agile-workflow)

validation.json → specs.md → repo-assessment.md → constitution.md → plan.md → tasks.md → …

## Eval gate by artifact

| Artifact | Stage eval file (under `{schema_root}/`) |
|----------|------------------------------------------|
| validation | Rubric in `templates/validation-template.md` only |
| specs | Skip (no stage eval) |
| repo-assessment | `evals/repo-assessment_eval.yaml` |
| constitution | Skip (input, not evaluated) |
| plan | `evals/plan_eval.yaml` |
| tasks | `evals/tasks_eval.yaml` |


## Guardrails

- ONE artifact per invocation (includes eval + refine + approval for that artifact)
- Do not skip eval gate for artifacts with `gate: stage_evals`
- Do not skip user approval
- Do not refine **templates** during eval gate — refine the **change artifact** only
- User rejection feedback loop **may** patch `{schema_root}/templates/` when required; write summaries to `feedback_stage_artifacts/`
- `target_repo` required before repo-assessment — **not** at `/opsx-new`
- Do not create the next artifact until the user approves the current one
- **No background sub-agents** — Do NOT launch background sub-agents, background shells, or Task-tool agents with `run_in_background=true` during `/opsx-continue`. Telemetry hooks execute in the main agent session only; background work cannot be metered and produces missing or incorrect metrics.

## Batch / Continue-All Telemetry

When the user requests "continue all" or approves multiple artifacts in a single session, use `--batch` flags on telemetry hooks so tokens are attributed at the phase level only:

- `python -m openspec.telemetry.auto on-artifact-start --change "<name>" --artifact "<artifact-id>" --phase <N> --batch`
- `python -m openspec.telemetry.auto on-artifact-created --change "<name>" --artifact "<artifact-id>" --phase <N> --batch`
- `python -m openspec.telemetry.auto on-artifact-complete --change "<name>" --artifact "<artifact-id>" --phase <N> --status passed --score <eval_score> --label "<quality_label>" --batch`

In batch mode, per-artifact `phase_progress` token updates are skipped. The final `on-artifact-complete --batch` for the last artifact in a phase uses `estimate_artifact_phase_tokens()` to compute a single honest phase total.
