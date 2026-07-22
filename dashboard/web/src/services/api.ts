import { API_BASE_URL } from "@/config";
import type {
  PipelineRun,
  PhaseExecution,
  TaskExecution,
  AgentEvent,
  GlobalHealthMetrics,
  ArtifactEditsOut,
  VerificationSummaryOut,
  RunReportOut,
} from "@/types";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE_URL}${path}`, {
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  if (!res.ok) {
    throw new Error(`API ${res.status}: ${await res.text()}`);
  }
  return res.json();
}

export async function fetchRuns(): Promise<{
  runs: PipelineRun[];
  total: number;
}> {
  return request("/runs");
}

export async function fetchRun(id: string): Promise<PipelineRun> {
  return request(`/runs/${id}`);
}

export async function fetchPhases(runId: string): Promise<PhaseExecution[]> {
  return request(`/runs/${runId}/phases`);
}

export async function fetchTasks(runId: string): Promise<TaskExecution[]> {
  return request(`/runs/${runId}/tasks`);
}

export async function fetchEvents(
  runId: string,
  limit = 100
): Promise<AgentEvent[]> {
  return request(`/events?run_id=${runId}&limit=${limit}`);
}

export async function fetchGlobalHealth(
  runId: string
): Promise<GlobalHealthMetrics> {
  return request(`/metrics/global/${runId}`);
}

export async function fetchArtifactEdits(
  runId: string
): Promise<ArtifactEditsOut> {
  return request(`/metrics/artifact-edits/${runId}`);
}

export async function fetchVerificationSummary(
  runId: string
): Promise<VerificationSummaryOut> {
  return request(`/metrics/verification-summary/${runId}`);
}

export async function fetchRunReport(runId: string): Promise<RunReportOut> {
  return request(`/runs/${runId}/report`);
}

export async function fetchLocalReport(runId: string): Promise<Record<string, unknown>> {
  return request(`/runs/${runId}/local-report`);
}

