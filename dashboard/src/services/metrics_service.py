from __future__ import annotations

from datetime import datetime, timezone
from typing import Any

from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from pathlib import Path

from src.core.config import AppConfig
from src.models.phase import PhaseExecution, PhaseStatus
from src.models.run import PipelineRun
from src.models.task import TaskExecution, TaskStatus
from src.schemas.metrics import (
    ArtifactEditEntry,
    ArtifactEditsOut,
    GlobalHealthMetrics,
    TaskVerificationEntry,
    VerificationSummaryOut,
)
from src.core.paths import get_change_dir
from openspec.telemetry.change_metrics import (
    ARTIFACT_PHASE_MAP,
    count_feedback_rounds,
    read_eval_refinement_round,
)


def _utc_now() -> datetime:
    return datetime.now(timezone.utc)


def _as_utc(dt: datetime) -> datetime:
    """Treat naive DB timestamps as UTC (SQLite stores UTC without tzinfo)."""
    if dt.tzinfo is None:
        return dt.replace(tzinfo=timezone.utc)
    return dt


async def compute_global_health(
    db: AsyncSession,
    run_id: str,
    cfg: AppConfig,
) -> GlobalHealthMetrics:
    run = await db.get(PipelineRun, run_id)
    if run is None:
        return GlobalHealthMetrics(
            total_tokens_consumed=0,
            total_run_cost_usd=0.0,
            cumulative_wall_time_s=0.0,
            agent_processing_time_s=0.0,
            compliance_index=cfg.fallbacks.default_compliance_index,
            gate_passing_rate=cfg.fallbacks.default_gate_pass_rate,
            human_rejection_rate=0.0,
            total_refinement_iterations=0,
            agent_success_rate=cfg.fallbacks.default_agent_success_rate,
            tasks_passed=0,
            tasks_total=0,
        )

    wall_time = 0.0
    if run.started_at:
        start = _as_utc(run.started_at)
        end = _as_utc(run.completed_at) if run.completed_at else _utc_now()
        wall_time = max(0.0, (end - start).total_seconds())

    phases_q = await db.execute(
        select(PhaseExecution).where(PhaseExecution.run_id == run_id)
    )
    phases = phases_q.scalars().all()

    phase_tokens_in = sum(p.tokens_in for p in phases)
    phase_tokens_out = sum(p.tokens_out for p in phases)
    total_tokens = phase_tokens_in + phase_tokens_out
    if total_tokens == 0:
        total_tokens = run.total_tokens_in + run.total_tokens_out

    total_phases = len(phases)
    first_pass = sum(1 for p in phases if p.iteration_count == 1 and p.status == PhaseStatus.passed)
    gate_passing = (first_pass / total_phases * 100) if total_phases > 0 else cfg.fallbacks.default_gate_pass_rate

    scored_phases = [p for p in phases if p.quality_score > 0]
    compliance = (
        (sum(p.quality_score for p in scored_phases) / len(scored_phases))
        if scored_phases
        else cfg.fallbacks.default_compliance_index
    )
    total_refinement = sum(max(0, p.iteration_count - 1) for p in phases)
    human_rejection = (total_refinement / total_phases * 100) if total_phases > 0 else 0.0

    phase_duration = sum(p.duration_s for p in phases if p.duration_s)
    if phase_duration > wall_time:
        wall_time = phase_duration

    # Agent success rate: tasks passed / tasks total
    tasks_q = await db.execute(
        select(TaskExecution).where(TaskExecution.run_id == run_id)
    )
    tasks = tasks_q.scalars().all()
    tasks_total = len(tasks)
    tasks_passed = sum(1 for t in tasks if t.status == TaskStatus.passed)
    success_rate = (tasks_passed / tasks_total * 100) if tasks_total > 0 else cfg.fallbacks.default_agent_success_rate

    cost = run.total_cost_usd
    if cost == 0 and total_tokens > 0:
        default_cost = cfg.metrics.cost_for_model("default")
        cost = (phase_tokens_in * default_cost.input + phase_tokens_out * default_cost.output) / 1_000_000

    task_cost = sum(t.cost_usd for t in tasks)
    if task_cost > cost:
        cost = task_cost

    if run.total_tokens_in == 0 and total_tokens > 0:
        run.total_tokens_in = phase_tokens_in
        run.total_tokens_out = phase_tokens_out
        run.total_cost_usd = cost
        await db.commit()

    agent_proc_time = sum(p.processing_time_s for p in phases if p.processing_time_s)

    return GlobalHealthMetrics(
        total_tokens_consumed=total_tokens,
        total_run_cost_usd=round(cost, 4),
        cumulative_wall_time_s=wall_time,
        agent_processing_time_s=round(agent_proc_time, 1),
        compliance_index=round(compliance, 1),
        gate_passing_rate=round(gate_passing, 1),
        human_rejection_rate=round(human_rejection, 1),
        total_refinement_iterations=total_refinement,
        agent_success_rate=round(success_rate, 1),
        tasks_passed=tasks_passed,
        tasks_total=tasks_total,
    )


async def compute_artifact_edits(
    db: AsyncSession,
    run_id: str,
    cfg: AppConfig,
) -> ArtifactEditsOut:
    run = await db.get(PipelineRun, run_id)
    if run is None:
        return ArtifactEditsOut(artifacts=[], total_edits=0)

    change_slug = run.change_name.split(" — ", 1)[-1] if " — " in run.change_name else run.change_name
    change_dir = get_change_dir(cfg, change_slug)
    if not change_dir.is_dir():
        return ArtifactEditsOut(artifacts=[], total_edits=0)

    entries: list[ArtifactEditEntry] = []
    for artifact_id, (_phase_num, phase_name, _is_last) in ARTIFACT_PHASE_MAP.items():
        artifact_path = change_dir / f"{artifact_id}.md"
        if not artifact_path.exists():
            artifact_path = change_dir / f"{artifact_id}.json"
        if not artifact_path.exists():
            continue
        eval_ref = read_eval_refinement_round(change_dir, artifact_id)
        fb = count_feedback_rounds(change_dir, artifact_id)
        entries.append(
            ArtifactEditEntry(
                artifact_id=artifact_id,
                phase_name=phase_name,
                eval_refinements=eval_ref,
                feedback_rounds=fb,
                total_edits=eval_ref + fb,
            )
        )

    return ArtifactEditsOut(
        artifacts=entries,
        total_edits=sum(e.total_edits for e in entries),
    )


def _read_verification_from_filesystem(
    change_dir: Path,
    task_id: str,
) -> dict[str, Any]:
    """Read verification results from state.yaml completed[] and task-reports/<task-id>.md.

    Merges both sources: state.yaml provides the baseline, task-reports
    fills in any missing command/output fields.
    """
    import re

    out: dict[str, Any] = {}

    # Source 1: state.yaml completed entries
    state_path = change_dir / "implementation" / "state.yaml"
    if state_path.exists():
        try:
            import yaml
            data = yaml.safe_load(state_path.read_text()) or {}
            current = data.get("current_task_result") or {}
            if current.get("task_id") == task_id:
                result = current
            else:
                result = {}
                for entry in data.get("completed", []):
                    if entry.get("task_id") == task_id:
                        result = entry
                        break
            if result:
                if "verification_pass" in result:
                    out["verification_pass"] = bool(result["verification_pass"])
                if "test_command" in result:
                    out["verification_command"] = str(result["test_command"])
                if "test_result" in result:
                    out["verification_result"] = str(result["test_result"])
                    if "verification_pass" not in out:
                        out["verification_pass"] = result["test_result"].upper() in ("PASS", "PASSED")
                if "test_output_summary" in result:
                    out["verification_output"] = str(result["test_output_summary"])[:2000]
        except Exception:
            pass

    all_populated = (
        out.get("verification_pass") is not None
        and out.get("verification_command")
        and out.get("verification_result")
        and out.get("verification_output")
    )
    if all_populated:
        return out

    # Source 2: task-reports/<task-id>.md — fill in gaps
    report_path = change_dir / "implementation" / "task-reports" / f"{task_id}.md"
    if not report_path.exists():
        return out
    try:
        content = report_path.read_text()
    except OSError:
        return out

    verif_match = re.search(r'##\s+Verification\s*\n(.*?)(?=\n##|\Z)', content, re.DOTALL)
    if not verif_match:
        return out
    table_text = verif_match.group(1)
    rows = re.findall(r'\|\s*(.+?)\s*\|\s*(PASSED|FAILED|PASS|FAIL)\s*\|', table_text, re.IGNORECASE)
    if not rows:
        return out

    all_passed = all(r[1].upper() in ("PASSED", "PASS") for r in rows)
    any_failed = any(r[1].upper() in ("FAILED", "FAIL") for r in rows)
    checks = [f"{r[0].strip()}: {r[1].strip()}" for r in rows]
    if out.get("verification_pass") is None:
        out["verification_pass"] = all_passed and not any_failed
    if not out.get("verification_result"):
        out["verification_result"] = "PASS" if (all_passed and not any_failed) else "FAIL"
    if not out.get("verification_output"):
        out["verification_output"] = "; ".join(checks)[:2000]
    if not out.get("verification_command"):
        cmd_match = re.search(r'`((?:go\s+(?:test|build|vet)|make\s+\S+|bash\s+-n)[^`]*)`', content)
        if cmd_match:
            out["verification_command"] = cmd_match.group(1)[:512]
    return out


async def compute_verification_summary(
    db: AsyncSession,
    run_id: str,
    cfg: AppConfig,
) -> VerificationSummaryOut:
    run = await db.get(PipelineRun, run_id)
    change_dir: Path | None = None
    if run:
        change_slug = run.change_name.split(" — ", 1)[-1] if " — " in run.change_name else run.change_name
        cd = get_change_dir(cfg, change_slug)
        if cd.is_dir():
            change_dir = cd

    result = await db.execute(
        select(TaskExecution).where(TaskExecution.run_id == run_id)
    )
    tasks = result.scalars().all()

    entries: list[TaskVerificationEntry] = []
    for t in tasks:
        v_pass = t.verification_pass
        v_cmd = t.verification_command
        v_result = t.verification_result
        v_output = t.verification_output

        if v_pass is None and not v_cmd and change_dir:
            fs_data = _read_verification_from_filesystem(change_dir, t.task_id)
            if fs_data:
                v_pass = fs_data.get("verification_pass")
                v_cmd = fs_data.get("verification_command", "")
                v_result = fs_data.get("verification_result", "")
                v_output = fs_data.get("verification_output", "")

        if v_pass is None and not v_cmd:
            continue

        entries.append(
            TaskVerificationEntry(
                task_id=t.task_id,
                task_title=t.task_title,
                verification_pass=v_pass,
                verification_command=v_cmd or "",
                verification_result=v_result or "",
                verification_output=v_output or "",
            )
        )

    total_verified = len(entries)
    total_passed = sum(1 for e in entries if e.verification_pass is True)
    total_failed = sum(1 for e in entries if e.verification_pass is False)

    return VerificationSummaryOut(
        entries=entries,
        total_verified=total_verified,
        total_passed=total_passed,
        total_failed=total_failed,
    )