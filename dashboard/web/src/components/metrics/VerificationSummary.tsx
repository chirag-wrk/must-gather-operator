import type { VerificationSummaryOut } from "@/types";

interface VerificationSummaryProps {
  data: VerificationSummaryOut | null;
}

export default function VerificationSummary({ data }: VerificationSummaryProps) {
  if (!data || data.entries.length === 0) return null;

  return (
    <section className="px-6 py-4 border-t border-terminal-border">
      <div className="text-terminal-muted text-xs uppercase tracking-wider mb-3 font-bold">
        Verification Results
        <span className="ml-2 text-terminal-text font-normal normal-case">
          {data.total_passed}/{data.total_verified} passed
          {data.total_failed > 0 && (
            <span className="text-terminal-red ml-1">
              ({data.total_failed} failed)
            </span>
          )}
        </span>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-terminal-border text-terminal-muted text-xs uppercase tracking-wider">
              <th className="px-3 py-2 text-left">Task</th>
              <th className="px-3 py-2 text-left">Result</th>
              <th className="px-3 py-2 text-left">Command</th>
              <th className="px-3 py-2 text-left">Output</th>
            </tr>
          </thead>
          <tbody>
            {data.entries.map((entry) => (
              <tr
                key={entry.task_id}
                className="border-b border-terminal-border/50 hover:bg-terminal-surface/50 transition-colors"
              >
                <td className="px-3 py-2">
                  <span className="font-mono text-terminal-accent">
                    {entry.task_id}
                  </span>
                  {entry.task_title && (
                    <span className="text-terminal-muted ml-1 text-xs">
                      {entry.task_title}
                    </span>
                  )}
                </td>
                <td className="px-3 py-2">
                  {entry.verification_pass === true && (
                    <span className="text-terminal-green">PASS</span>
                  )}
                  {entry.verification_pass === false && (
                    <span className="text-terminal-red">FAIL</span>
                  )}
                  {entry.verification_pass === null && (
                    <span className="text-terminal-muted">—</span>
                  )}
                  {entry.verification_result && (
                    <span className="text-terminal-muted text-xs ml-1">
                      ({entry.verification_result})
                    </span>
                  )}
                </td>
                <td className="px-3 py-2 font-mono text-xs text-terminal-muted max-w-xs truncate">
                  {entry.verification_command || "—"}
                </td>
                <td className="px-3 py-2 text-xs text-terminal-muted max-w-md truncate">
                  {entry.verification_output || "—"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}
