import type { PhaseExecution } from "@/types";
import { PHASE_DISPLAY_NAMES, STATUS_ICONS } from "@/types";

interface PhaseRowProps {
  phase: PhaseExecution;
}

function formatTokens(n: number): string {
  if (n >= 1_000) return `${Math.round(n / 1_000)}k`;
  return String(n);
}

function formatDuration(seconds: number): string {
  if (seconds === 60) return "~1m (est.)";
  if (seconds === 0) return "\u2014";
  const m = Math.floor(seconds / 60);
  const s = Math.floor(seconds % 60);
  return `${m}m ${s.toString().padStart(2, "0")}s`;
}

export default function PhaseRow({ phase }: PhaseRowProps) {
  const statusIcon = STATUS_ICONS[phase.status];
  const statusLabel = phase.status.toUpperCase();
  const isLooping = phase.iteration_count > 3;

  return (
    <tr className="border-b border-terminal-border hover:bg-terminal-surface/50 transition-colors">
      <td className="px-4 py-2.5 text-sm">
        <span className="text-terminal-muted mr-1">{phase.phase_number}.</span>
        {PHASE_DISPLAY_NAMES[phase.phase_name]}
      </td>
      <td className="px-4 py-2.5 text-sm">
        {statusIcon}{" "}
        <span
          className={
            phase.status === "passed"
              ? "text-terminal-green"
              : phase.status === "failed"
              ? "text-terminal-red"
              : phase.status === "waiting"
              ? "text-terminal-yellow"
              : "text-terminal-text"
          }
        >
          {statusLabel}
        </span>
        {phase.phase_name === "code_generation" && phase.status === "running" && (
          <span className="text-terminal-muted text-xs ml-1">(closes on impl. report)</span>
        )}
      </td>
      <td className="px-4 py-2.5 text-sm text-center">
        {phase.iteration_count} {phase.iteration_count > 1 ? "Iterations" : "Iteration"}
        {isLooping && " 🔄"}
      </td>
      <td className="px-4 py-2.5 text-sm text-center">
        {formatDuration(phase.duration_s)}
      </td>
      <td className="px-4 py-2.5 text-sm text-center font-mono">
        {formatTokens(phase.tokens_in)} / {formatTokens(phase.tokens_out)}
      </td>
      <td className="px-4 py-2.5 text-sm">
        {phase.quality_label || `Score: ${phase.quality_score}/100`}
      </td>
    </tr>
  );
}
