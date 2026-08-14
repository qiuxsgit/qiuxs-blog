import { expect, it } from "vitest";

import { ApiProblem } from "../api/problem";
import { operationProblem } from "./operation-problem";

it("preserves an API Problem for safe operation presentation", () => {
  const problem = new ApiProblem(503, "article_unavailable", "req-article", "Article unavailable");

  expect(operationProblem(problem, "Fallback", "fallback_code")).toBe(problem);
});

it("normalizes an unknown failure without exposing its message", () => {
  const problem = operationProblem(new Error("secret backend detail"), "Unable to create article", "create_article_failed");

  expect(problem).toMatchObject({
    status: 503,
    title: "Unable to create article",
    code: "create_article_failed",
    requestId: "client",
  });
  expect(problem.message).not.toContain("secret backend detail");
});
