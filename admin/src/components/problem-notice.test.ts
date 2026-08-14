import { describe, expect, it } from "vitest";
import { ApiProblem } from "../api/problem";
import { problemNoticeModel } from "./problem-notice";

describe("problem notice mapping", () => {
  it("sanitizes unsafe fields and uses specialized service messaging", () => {
    const model = problemNoticeModel(new ApiProblem(409, "builder_conflict\u0000", "req\n1", "Unsafe\u0007 title"));
    expect(model.heading).toBe("Unsafe  title");
    expect(model.code).toBe("builder_conflict");
    expect(model.requestId).toBe("req 1");
    const specialized = problemNoticeModel(new ApiProblem(409, "builder_conflict", "req", "Changed"));
    expect(specialized.heading).toContain("Builder configuration changed elsewhere");
    expect(specialized.detail).toBe("Changed");
  });
});
