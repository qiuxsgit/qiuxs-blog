import { describe, expect, it } from "vitest";
import { ApiProblem } from "../api/problem";
import {
  builderDraftFromView,
  builderLoadState,
  buildBuilderPutRequest,
  clearBuilderToken,
  builderProblemMessage,
  defaults,
  canTestBuilder,
  validateBuilderDraft,
  type BuilderDraft,
} from "./builder-model";

const valid: BuilderDraft = {
  ...defaults,
  name: "Production",
  baseUrl: "https://jenkins.example.com",
  username: "ci",
  jobName: "blog/site",
  token: "secret-token",
  enabled: true,
  tokenConfigured: true,
};

describe("builder settings pure model", () => {
  it("accepts only canonical HTTPS origins and safe Jenkins job paths", () => {
    expect(validateBuilderDraft(valid)).toEqual([]);
    for (const baseUrl of ["https://jenkins.example.com/", "https://EXAMPLE.com", "http://jenkins.example.com", "https://user:pass@jenkins.example.com", "https://jenkins.example.com/path", "https://jenkins.example.com?token=x", "https://[::1]", "https://jenkins.example.com:443", "https://192.168.001.1"]) {
      expect(validateBuilderDraft({ ...valid, baseUrl }), baseUrl).toContain("baseUrl");
    }
    for (const jobName of ["", "/job", "job//site", "job/.", "job/..", "job name", "job?x", "job/é"]) {
      expect(validateBuilderDraft({ ...valid, jobName })).toContain("jobName");
    }
  });

  it("enforces trimmed rune limits and token requirement for first configuration", () => {
    expect(validateBuilderDraft({ ...valid, name: " Production " })).toEqual([]);
    expect(validateBuilderDraft({ ...valid, username: " ci " })).toEqual([]);
    expect(buildBuilderPutRequest({ ...valid, name: " Production ", username: " ci " })).toMatchObject({ name: "Production", username: "ci" });
    expect(validateBuilderDraft({ ...valid, name: "😀".repeat(101) })).toContain("name");
    expect(validateBuilderDraft({ ...valid, username: "ci:bot" })).toContain("username");
    expect(validateBuilderDraft({ ...valid, token: "" }, false)).toContain("token");
    expect(validateBuilderDraft({ ...valid, token: "" }, true)).not.toContain("token");
    expect(validateBuilderDraft({ ...valid, token: "😀".repeat(4097) })).toContain("token");
  });

  it("builds the exact PUT body and omits blank token and readonly fields", () => {
    const request = buildBuilderPutRequest({ ...valid, token: "" });
    expect(request).toEqual({ name: "Production", baseUrl: "https://jenkins.example.com", username: "ci", jobName: "blog/site", enabled: true });
    expect(request).not.toHaveProperty("id");
    expect(request).not.toHaveProperty("token");
    expect(buildBuilderPutRequest(valid)).toHaveProperty("token", "secret-token");
  });

  it("treats GET 404 as an empty editable configuration without fabricating cache data", () => {
    expect(defaults.enabled).toBe(false);
    expect(builderLoadState(new ApiProblem(404, "not_found", "r1", "Not found"))).toEqual({ kind: "empty" });
    expect(builderLoadState(new ApiProblem(503, "dependency_unavailable", "r2", "Unavailable"))).toEqual({ kind: "error", error: expect.any(ApiProblem) });
    expect(builderLoadState(undefined)).toEqual({ kind: "configured" });
  });

  it("clears the token after successful save and only enables test for saved enabled config", () => {
    expect(clearBuilderToken(valid)).toEqual({ ...valid, token: "" });
    const savedView = { id: 1, name: "Production", baseUrl: "https://jenkins.example.com", username: "ci", jobName: "blog/site", enabled: true, tokenConfigured: true };
    expect(canTestBuilder(savedView, { ...valid, token: "" }, true)).toBe(true);
    expect(canTestBuilder(savedView, { ...valid, token: "secret-token" }, true)).toBe(false);
    expect(canTestBuilder(savedView, { ...valid, name: "Changed", token: "" }, true)).toBe(false);
    expect(canTestBuilder(savedView, { ...valid, enabled: false, token: "" }, true)).toBe(false);
    expect(canTestBuilder(savedView, { ...valid, token: "" }, false)).toBe(false);
    expect(canTestBuilder(null, { ...valid, token: "" }, true)).toBe(false);
  });

  it("maps only safe problem metadata and never echoes a token", () => {
    const problem = new ApiProblem(409, "builder_conflict", "req-1", "Builder conflict secret-token");
    expect(builderProblemMessage(problem)).not.toContain("secret-token");
    expect(problem).toMatchObject({ status: 409, code: "builder_conflict", requestId: "req-1" });
  });
});
