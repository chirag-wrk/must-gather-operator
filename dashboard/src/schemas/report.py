from __future__ import annotations

from datetime import datetime

from pydantic import BaseModel

from src.schemas.event import EventOut
from src.schemas.metrics import ArtifactEditsOut, GlobalHealthMetrics, VerificationSummaryOut
from src.schemas.phase import PhaseOut
from src.schemas.run import RunOut
from src.schemas.task import TaskOut


class RunReportOut(BaseModel):
    exported_at: datetime
    run: RunOut
    phases: list[PhaseOut]
    tasks: list[TaskOut]
    events: list[EventOut]
    global_health: GlobalHealthMetrics
    artifact_edits: ArtifactEditsOut
    verification_summary: VerificationSummaryOut
