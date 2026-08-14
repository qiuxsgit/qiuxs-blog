import type { ApiProblem } from "../api/problem";

const specializedMessages: Record<string, string> = {
  builder_conflict: "Builder configuration changed elsewhere. Review the current configuration and try again.",
};

function safeText(value: string): string {
  return value.replace(/[\u0000-\u001f\u007f]/g, " ").trim() || "Unavailable";
}

export function ProblemNotice({ problem }: { problem: ApiProblem }) {
  const specializedMessage = specializedMessages[problem.code];
  return (
    <section className="problem-notice" role="alert">
      <span aria-hidden="true" className="problem-icon">⚠</span>
      <div>
        <h2>{specializedMessage ?? safeText(problem.title)}</h2>
        {specializedMessage && <p>{safeText(problem.title)}</p>}
        <p className="problem-meta">Code: {safeText(problem.code)} · Request ID: {safeText(problem.requestId)}</p>
      </div>
    </section>
  );
}
