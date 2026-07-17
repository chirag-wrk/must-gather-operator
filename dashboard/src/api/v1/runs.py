from __future__ import annotations

import json

from fastapi import APIRouter, HTTPException
from fastapi.responses import JSONResponse
from sqlalchemy import select, func

from src.core.dependencies import DBSession, Settings
from src.core.paths import get_change_dir
from src.models.run import PipelineRun
from src.schemas.report import RunReportOut
from src.schemas.run import RunCreate, RunUpdate, RunOut, RunListOut
from src.services.pipeline_scanner import scan_changes
from src.services.report_service import build_run_report

router = APIRouter(prefix="/runs", tags=["runs"])


@router.get("", response_model=RunListOut)
async def list_runs(db: DBSession):
    result = await db.execute(
        select(PipelineRun).order_by(PipelineRun.created_at.desc())
    )
    runs = result.scalars().all()
    count_result = await db.execute(select(func.count(PipelineRun.id)))
    total = count_result.scalar() or 0
    return RunListOut(
        runs=[RunOut.model_validate(r) for r in runs],
        total=total,
    )


@router.get("/{run_id}", response_model=RunOut)
async def get_run(run_id: str, db: DBSession):
    run = await db.get(PipelineRun, run_id)
    if run is None:
        raise HTTPException(status_code=404, detail="Run not found")
    return RunOut.model_validate(run)


@router.get("/{run_id}/report", response_model=RunReportOut)
async def export_run_report(run_id: str, db: DBSession, cfg: Settings):
    return await build_run_report(db, run_id, cfg)


@router.get("/{run_id}/local-report")
async def get_local_report(run_id: str, db: DBSession, cfg: Settings):
    """Serve the local metrics-report.json file for a run."""
    run = await db.get(PipelineRun, run_id)
    if run is None:
        raise HTTPException(status_code=404, detail="Run not found")

    change_slug = run.change_name.split(" — ", 1)[-1] if " — " in run.change_name else run.change_name
    report_path = get_change_dir(cfg, change_slug) / "telemetry" / "metrics-report.json"

    if not report_path.exists():
        raise HTTPException(status_code=404, detail="Local metrics-report.json not found")

    try:
        data = json.loads(report_path.read_text())
    except (json.JSONDecodeError, OSError) as exc:
        raise HTTPException(status_code=500, detail=f"Failed to read report: {exc}")

    return JSONResponse(content=data)


@router.post("", response_model=RunOut, status_code=201)
async def create_run(payload: RunCreate, db: DBSession):
    run = PipelineRun(
        change_name=payload.change_name,
        jira_key=payload.jira_key,
        branch=payload.branch,
    )
    db.add(run)
    await db.commit()
    await db.refresh(run)
    return RunOut.model_validate(run)


@router.patch("/{run_id}", response_model=RunOut)
async def update_run(run_id: str, payload: RunUpdate, db: DBSession):
    run = await db.get(PipelineRun, run_id)
    if run is None:
        raise HTTPException(status_code=404, detail="Run not found")
    for field, value in payload.model_dump(exclude_unset=True).items():
        setattr(run, field, value)
    await db.commit()
    await db.refresh(run)
    return RunOut.model_validate(run)


@router.post("/scan", response_model=list[str])
async def scan_existing_changes(db: DBSession, cfg: Settings):
    return await scan_changes(db, cfg)
