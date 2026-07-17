from __future__ import annotations

from datetime import datetime
from pydantic import BaseModel

from src.models.task import TaskStatus


class TaskCreate(BaseModel):
    run_id: str
    phase_id: str
    task_id: str
    task_title: str = ""
    agent_id: str = ""


class TaskUpdate(BaseModel):
    status: TaskStatus | None = None
    self_correction_loops: int | None = None
    tokens_in: int | None = None
    tokens_out: int | None = None
    cost_usd: float | None = None
    token_attribution: str | None = None
    started_at: datetime | None = None
    completed_at: datetime | None = None


class TaskOut(BaseModel):
    id: str
    run_id: str
    phase_id: str
    task_id: str
    task_title: str
    agent_id: str
    status: TaskStatus
    self_correction_loops: int
    tokens_in: int
    tokens_out: int
    cost_usd: float
    token_attribution: str | None = None
    processing_time_s: float = 0.0
    verification_pass: bool | None = None
    verification_command: str = ""
    verification_result: str = ""
    verification_output: str = ""
    started_at: datetime | None
    completed_at: datetime | None

    model_config = {"from_attributes": True}
