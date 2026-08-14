import type { ApiProblem } from "../api/problem";
import { problemNoticeModel } from "./problem-notice";

export function ProblemNotice({ problem }: { problem: ApiProblem }) {
  const model = problemNoticeModel(problem);
  return (
    <section className="problem-notice" role="alert">
      <span aria-hidden="true" className="problem-icon">⚠</span>
      <div>
        <h2>{model.heading}</h2>
        {model.detail && <p>{model.detail}</p>}
        <p className="problem-meta">Code: {model.code} · Request ID: {model.requestId}</p>
      </div>
    </section>
  );
}
