import type { ApiProblem } from "../api/problem";

const specializedMessages: Record<string, string> = {
  builder_conflict: "Builder configuration changed elsewhere. Review the current configuration and try again.",
};

export function safeProblemText(value: string): string {
  return value.replace(/[\u0000-\u001f\u007f]/g, " ").trim() || "Unavailable";
}

export function problemNoticeModel(problem: ApiProblem): { heading: string; detail?: string | undefined; code: string; requestId: string } {
  const specialized = specializedMessages[problem.code];
  return { heading: specialized ?? safeProblemText(problem.title), detail: specialized ? safeProblemText(problem.title) : undefined, code: safeProblemText(problem.code), requestId: safeProblemText(problem.requestId) };
}
