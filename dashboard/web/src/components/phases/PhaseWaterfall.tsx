import type { PhaseExecution } from "@/types";
import PhaseRow from "./PhaseRow";

interface PhaseWaterfallProps {
  phases: PhaseExecution[];
}

export default function PhaseWaterfall({ phases }: PhaseWaterfallProps) {
  return (
    <section className="px-6 py-4 border-b border-terminal-border">
      <div className="text-terminal-muted text-xs uppercase tracking-wider mb-3 font-bold">
        Phase-by-Phase Telemetry &amp; Iteration Waterfall
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-left">
          <thead>
            <tr className="border-b-2 border-terminal-border text-terminal-muted text-xs uppercase">
              <th className="px-4 py-2">Phase</th>
              <th className="px-4 py-2">Status</th>
              <th className="px-4 py-2 text-center">Loops / Iter.</th>
              <th className="px-4 py-2 text-center">Time Taken</th>
              <th className="px-4 py-2 text-center">Tokens In / Out</th>
              <th className="px-4 py-2">Quality / Eval Output</th>
            </tr>
          </thead>
          <tbody>
            {phases.length === 0 ? (
              <tr>
                <td
                  colSpan={6}
                  className="px-4 py-8 text-center text-terminal-muted text-sm"
                >
                  No phase data available.
                </td>
              </tr>
            ) : (
              phases.map((p) => <PhaseRow key={p.id} phase={p} />)
            )}
          </tbody>
        </table>
      </div>
    </section>
  );
}
