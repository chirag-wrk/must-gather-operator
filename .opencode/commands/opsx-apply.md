---
name: /opsx:apply
id: opsx-apply
category: Workflow
description: Implement tasks — one task per invocation, state machine driven (supports ai-helpers and direct modes)
---

Implement an OpenSpec change. **ONE task per invocation.**
State-machine driven with externalized state at `implementation/state.yaml`.

**Mode**: Read `codegen_mode` from `openspec/config.yaml` → `flags.codegen_mode`:
- `ai-helpers` — OAPE command routing + code-generation eval gate
- `direct` — plain agent implementation, no OAPE commands, no code eval gate

**Per-task flow (ai-helpers):** OAPE → verify → tests → eval gate → refine → present → YIELD → wait for next invocation.
**Per-task flow (direct):** implement → verify → present → YIELD → wait for next invocation.

**Reference (ai-helpers only):** schema `oape_routing`, `code_generation_eval_gate`, `{schema_root}/stage-gate/CODE_GENERATION_EVAL_PROMPT.md`

**Input**: Optionally specify a change name (e.g., `/opsx:apply cm-830`). If omitted, infer from context or prompt.

## Architecture: State Machine

```
ai-helpers mode:  IDLE → EXECUTING_TASK → RUNNING_TESTS → EVAL_GATE → AWAITING_APPROVAL → IDLE → ... → PHASE_COMPLETE → IDLE/COMPLETE
direct mode:      IDLE → EXECUTING_TASK → RUNNING_TESTS → AWAITING_APPROVAL → IDLE → ... → PHASE_COMPLETE → IDLE/COMPLETE
```

The orchestrator reads state, executes ONE task, writes state, and YIELDS.
It NEVER advances to the next task within the same response.

## State File

Location: `openspec/changes/<name>/implementation/state.yaml`
Template: `{schema_root}/templates/implementation-state-template.yaml`

Initialize from template on first invocation if missing.

## HARD RULES — NON-NEGOTIABLE

1. **Read `state.yaml` FIRST** — before any other action, every single invocation
2. **Read `codegen_mode`** — from `openspec/config.yaml` → `flags.codegen_mode` (default: `direct`)
3. **ONE task per invocation** — you MUST NOT execute more than one task in a single response. When you finish presenting a task for approval, your response is DONE. Period.
4. **YIELD = END YOUR RESPONSE** — after the approval question, you MUST stop generating text. Do not read the next task. Do not compose the next design bundle. Do not think about what comes next. YOUR RESPONSE ENDS.
5. **On user "approve"** — write task report, mark complete, update state to IDLE, then STOP. Tell the user to run `/opsx-apply` again. Do NOT start the next task.
6. **Context windowing** — only load §4 payload for `current_task_id`, not all tasks
7. **Write state after every transition** — state must survive agent crashes
8. **No background sub-agents** — Do NOT launch background sub-agents, background shells, or Task-tool agents with `run_in_background=true` during `/opsx-apply`. Telemetry hooks execute in the main agent session only; background work cannot be metered and produces missing or incorrect metrics.

## YIELD BOUNDARY — CRITICAL

When you reach the approval question, you have TWO possible next actions:
- If user has NOT yet responded → END YOUR RESPONSE after the question
- If user says "approve" → write report, mark done, say "Run `/opsx-apply` for next task", then END YOUR RESPONSE
- If user says "reject" → re-run THIS task only (not the next one)

**WHAT YIELD MEANS:** You literally stop generating output. No "let me also...", no "now moving to...", no "next up...". The response terminates. The user must send a NEW message or re-invoke `/opsx-apply` to trigger the next task.

**WHY:** Without YIELD, you will batch tasks together. This destroys the per-task approval flow. The user MUST be able to review each task's code in isolation before the next one starts.

## Steps

### 1. Read state and config

Read `openspec/changes/<name>/implementation/state.yaml`.
If file doesn't exist, initialize from template.

Read `openspec/config.yaml` → `flags.codegen_mode`. If not set, default to `direct`.

### 2. Handle current state

| State | Action |
|-------|--------|
| `IDLE` | Pick next pending task → go to step 3 |
| `AWAITING_APPROVAL` | Read user response (approve/reject) → handle |
| `PHASE_COMPLETE` | Offer optional PR for this phase → advance to next phase or COMPLETE |
| `COMPLETE` | Announce done, suggest `/opsx-archive` → STOP |
| `EXECUTING_TASK` | Resume from crash — re-run current task |

**On approve** (from AWAITING_APPROVAL):
- Write `implementation/task-reports/<task-id>.md`
- Mark task `- [x]` in tasks.md
- Move `current_task_result` to `completed[]`
- Clear `current_task_result` and `rejections`
- **Telemetry — signal task complete** (silent, non-blocking; rolls up phase-5 tokens incrementally):
  ```bash
  python -m openspec.telemetry.auto on-task-complete --change "<name>" --task-id "<TASK_ID>" --status passed --phase <N>
  ```
- Set state: `IDLE`
- Check if all tasks done → set `COMPLETE` if yes
- Output EXACTLY: "Task {id} approved. Report written. State: IDLE.\n\nRun `/opsx-apply` to execute the next task."
- **>>> STOP. END RESPONSE. DO NOT CONTINUE. <<<**

**On reject** (from AWAITING_APPROVAL):
- Append feedback to `rejections[]`
- Set state: `EXECUTING_TASK`
- **ai-helpers mode**: Add REVISION FEEDBACK to design-bundle
- **direct mode**: Incorporate feedback into implementation approach
- Continue to step 3 (re-execute current task)

### 3. Select change and verify (first invocation only)

On first run (no state.yaml):
1. Select change (`openspec list --json` if name not given)
2. `openspec status --change "<name>" --json`
3. Verify prerequisites:
   - **ai-helpers mode**: OAPE commands in `.cursor/commands/`, artifacts approved, gh/go/git/make available
   - **direct mode**: artifacts approved, go/git/make available
4. Fork setup: read `inputs/jira.yaml`, clone fork, create feature branch
5. Create `implementation/` and `task-reports/` dirs
6. Parse tasks.md §2 order, set `total_tasks`
7. Initialize `state.yaml` with state: IDLE
8. **Telemetry — signal apply start / phase 5** (silent, non-blocking):
   ```bash
   python -m openspec.telemetry.auto on-apply-start --change "<name>" --phase <N>
   ```
   Where `<N>` is `current_plan_phase` from state.yaml.
9. Pick first pending task for the current phase → continue to step 4

### 4. Execute ONE task

**Context windowing**: Read ONLY the §4 payload for `current_task_id` from tasks.md.
Do NOT read payloads for other tasks.

Set state: `EXECUTING_TASK`. Write state.yaml.

**Telemetry — signal task start** (silent, non-blocking):
```bash
python -m openspec.telemetry.auto on-task-start --change "<name>" --task-id "<TASK_ID>" --agent "<AGENT_ID>" --title "<task_title>" --phase <N>
```

---

<!-- ╔══════════════════════════════════════════════════════════════╗ -->
<!-- ║  MODE BRANCH: codegen_mode determines steps 4a–4e          ║ -->
<!-- ╚══════════════════════════════════════════════════════════════╝ -->

#### IF codegen_mode = ai-helpers

##### 4a. Compose design bundle

Write `implementation/design-bundle.md`:
- constitution, specs, plan, repo-assessment excerpts
- §4 payload **ONLY for current Task ID**
- REVISION FEEDBACK if retrying after rejection

##### 4b. Run OAPE command (exactly one)

1. **IF e2e task** → `/oape:e2e-generate <fork-default-branch>`
2. **ELIF** `API_Agent` verification-only → `/oape:api-generate-tests <api-path>`
3. **ELIF** `API_Agent` → `/oape:api-generate --design-doc <bundle>` + `make update && make verify`
4. **ELIF** `OperatorController_Agent` → `/oape:api-implement --design-doc <bundle>`
5. **ELIF** manual agent → implement task payload directly

##### 4c. Verify and test

Set state: `RUNNING_TESTS`. Write state.yaml.

Run Makefile targets from this task's Acceptance criteria. Test tier classification:
- **Tier 1** (co-generate): Controller, API with webhooks/validation, manual Go with logic
  → co-generate `_test.go` → `go test ./<package>/... -v -count=1`
- **Tier 2** (run existing): Packages with existing `_test.go` coverage
  → `go test ./<package>/... -v -count=1`
- **Tier 3** (build verify): Pure struct types, codegen output, e2e
  → `go build` + `go vet` (+ `make verify` for codegen)
- **Tier 4** (non-Go): YAML, scripts, manifests
  → `make verify` or `bash -n`

##### 4d. Code eval gate

Set state: `EVAL_GATE`. Write state.yaml.

Read and follow **`{schema_root}/stage-gate/CODE_GENERATION_EVAL_PROMPT.md`** Steps 1–7 exactly.
This is the single source of truth for per-task code eval scoring, verification, test execution,
refinement, and result recording. Key paths used by the prompt:
- Eval cases: `{schema_root}/evals/code-generation_eval.yaml` (filter by oape_command)
- Eval results output: `openspec/changes/<name>/eval-results/code-generation-<task-id>.yaml`
- Task report template: `{schema_root}/templates/implementation-task-report-template.md`
- Max refinement passes: 2

##### 4e. Write result

Write `current_task_result` to state.yaml:
```yaml
current_task_result:
  task_id: <id>
  oape_command: <command>
  files_changed: [...]
  verification_pass: true/false
  test_command: "..."
  test_result: PASS/FAIL
  test_output_summary: "..."
  eval_score: <N>
  eval_cases_pass: <N>
  eval_cases_total: <N>
  refinement_rounds: <N>
```

---

#### ELSE (codegen_mode = direct)

##### 4a. Read context files

Read the following for architecture patterns, guardrails, and task-specific guidance:
- agents.md — architecture patterns, test exemplars, coding conventions
- constitution.md — guardrails and verification requirements
- specs.md — requirements traced by this task
- plan.md — phase goals and verification hooks
- repo-assessment.md — target files, reusable assets
- tasks.md §4 payload for **current Task ID only**
- REVISION FEEDBACK if retrying after rejection

##### 4b. Implement code directly

Apply code changes in the working copy following:
- agents.md patterns and conventions
- constitution.md guardrails
- Task payload instructions (objective, target files, implementation notes)
- Acceptance criteria from the task

##### 4c. Co-generate unit tests (mandatory for Tier 1 tasks)

For tasks producing Go source files with testable logic:
- Scan files_changed for new/modified `.go` files (excluding `_test.go`)
- For Tier 1 tasks: verify corresponding `_test.go` exists for each production `.go` file
- If any `_test.go` missing: generate it before proceeding (follow agents.md test exemplar)
- Run `go test ./<package>/... -v -count=1`
- If tests fail: fix code/tests and re-run (up to 2 attempts)
- Record test file paths + pass/fail in `current_task_result`
- Tier 2: run existing `go test` on modified packages
- Tier 3: `go build` + `go vet`
- Tier 4 (non-Go): `make verify` or `bash -n`

##### 4d. Verify and test

Set state: `RUNNING_TESTS`. Write state.yaml.

Run Makefile targets from this task's Acceptance criteria. Apply same tiered
classification as ai-helpers mode step 4c.

##### 4e. Write result

Write `current_task_result` to state.yaml:
```yaml
current_task_result:
  task_id: <id>
  files_changed: [...]
  verification_pass: true/false
  test_command: "..."
  test_result: PASS/FAIL
  test_output_summary: "..."
```

---

<!-- ╔══════════════════════════════════════════════════════════════╗ -->
<!-- ║  END MODE BRANCH — shared flow resumes                     ║ -->
<!-- ╚══════════════════════════════════════════════════════════════╝ -->

### 5. Present and YIELD

Set state: `AWAITING_APPROVAL`. Write state.yaml.

#### ai-helpers mode — presentation format

```
## Task: <TASK_ID> — <title>
Phase: <phase> | Task <index>/<total>

### OAPE Commands Executed
| Command | Args | Outcome |

### Files Changed
- path/to/file — brief description

### Test Results
| Test | Command | Result |

### Code Eval Scorecard
Score: N% (pass/total cases) | Refinement rounds: N

### Deviations (if any)
```

ASK: **"Code eval score: {N}% ({pass}/{total} cases pass). Approve the code changes for task {task_id} ({task_title})? (Approve / Reject with feedback)"**

#### direct mode — presentation format

```
## Task: <TASK_ID> — <title>
Phase: <phase> | Task <index>/<total>

### Files Changed
- path/to/file — brief description

### Test Results
| Test | Command | Result |

### Deviations (if any)
```

ASK: **"Approve the code changes for task {task_id} ({task_title})? (Approve / Reject with feedback)"**

---

**╔══════════════════════════════════════════════════════════════╗**
**║  >>> YIELD — STOP GENERATING. END YOUR RESPONSE NOW. <<<   ║**
**║  Do NOT read the next task. Do NOT compose another bundle.  ║**
**║  Do NOT continue with any other action.                     ║**
**║  The user must send a new message to proceed.               ║**
**╚══════════════════════════════════════════════════════════════╝**

### 6. Phase boundary (all current-phase tasks complete)

When all **current phase** tasks are marked complete:

1. Set state: `PHASE_COMPLETE`. Write state.yaml.
2. Present phase summary (tasks completed, files changed, test results for this phase).
3. **Telemetry — signal phase complete** (silent, non-blocking):
   ```bash
   python -m openspec.telemetry.auto on-phase-complete --change "<name>" --phase <N> --pr-raised <true|false>
   ```
4. ASK: **"All Phase {N} tasks complete. Raise a draft PR for Phase {N}? (Yes / No, continue to Phase {N+1})"**
5. If yes: commit, push, open draft PR scoped to this phase. Record URL in `state.yaml` → `phase_pr_urls`. **Working-folder mode:** skip push/PR.
6. Check if `current_plan_phase >= total_plan_phases`:
   - **All phases done:**
     - **Telemetry — signal apply complete:**
       ```bash
       python -m openspec.telemetry.auto on-apply-complete --change "<name>"
       ```
     - Write `implementation-report.md` aggregating all `task-reports/*.md`
     - Write `deviation-observed.md` if any deviations logged
     - Present final summary with all phase PR URLs
     - Set state: `COMPLETE`. Write state.yaml.
   - **Phases remain:**
     - Update `state.yaml`: `current_plan_phase = N+1`, state = `IDLE`
     - Output: "Phase {N} complete. Run `/opsx-continue` to generate Phase {N+1} tasks."
7. YIELD

## Guardrails

- **Read state.yaml FIRST** — every invocation, no exceptions
- **Read codegen_mode** — from config.yaml, every invocation
- **ONE task per response** — NEVER implement two tasks in one invocation, even if the user approves inline
- **YIELD after approval question** — HARD STOP. End your response. No exceptions.
- **YIELD after processing approval** — write report, say "run /opsx-apply", then HARD STOP. Do NOT start next task.
- **Context windowing** — only §4 for current task, never load all task payloads
- **Write state on every transition** — crash recovery
- **Mandatory test execution** — never skip verification or tests
- **Never advance without a fresh invocation** — even if user says "approve", you stop after recording it
- On reject: re-run current task only (full loop)
- **ai-helpers mode**: One OAPE command per task; OAPE in fork/working-folder cwd only

## Anti-Batching Contract

You are PROHIBITED from:
- Executing task N+1 in the same response where task N was approved
- Reading §4 payload for any task other than current_task_id
- Composing a design bundle for the next task after an approval (ai-helpers mode)
- Writing "now moving to..." or "let me start the next task..."
- Any action that advances the workflow after presenting an approval question or processing an approval

If you find yourself about to start a new task in the same response — STOP. You are violating the contract.

## Batch / Apply-All Telemetry

When the user requests "approve all", "continue all tasks", or similar batch execution that completes multiple tasks in a single session, per-task token estimation is unreliable (file-based estimation repeats the same shared context for every task). Use `--batch` flags on telemetry hooks so tokens are attributed at the phase level only:

1. At batch start: `python -m openspec.telemetry.auto on-apply-start --change "<name>" --phase <N> --batch`
2. Per task: still call `on-task-start --phase <N>` and `on-task-complete --phase <N> --batch` for each task (records status, agent, eval loops — but tokens_in/out = 0 with attribution = "phase_aggregate")
3. At end: `python -m openspec.telemetry.auto on-apply-complete --change "<name>"` (phase-level tokens computed once, not summed per-task)
4. Do **not** expect per-task token breakdown in metrics for batch runs

**Auto-detect fallback:** If `--batch` is accidentally omitted, `on-apply-complete` auto-detects batch mode when 2+ tasks have near-identical token estimates and corrects to phase-level attribution.
