import { ApiProblem } from "../api/problem";

export function operationProblem(error: unknown, title: string, code: string): ApiProblem {
  return error instanceof ApiProblem ? error : new ApiProblem(503, code, "client", title);
}
