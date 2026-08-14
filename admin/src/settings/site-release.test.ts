import { describe, expect, it } from "vitest";
import { confirmedPutFields, conflictLocalDraft, isSettingsConflict, publishSettingsRequest } from "./site-release";
import { ApiProblem } from "../api/problem";
import { defaults, type SiteDraft } from "./site-model";

describe("site settings release helpers", () => {
  it("preserves local PUT fields on settings conflict", () => {
    const local: SiteDraft = { ...defaults, siteName: "local" };
    const confirmed: SiteDraft = { ...defaults, siteName: "server", authorName: "server author" };
    expect(conflictLocalDraft(local, confirmed)).toEqual(local);
  });

  it("creates a separate settings release without article inference", () => {
    expect(publishSettingsRequest()).toEqual({ mode: "publish_settings", articleId: null });
  });

  it("copies only confirmed PUT fields during conflict recovery", () => {
    const confirmed: SiteDraft = { ...defaults, id: 12, updatedAt: "2026-01-01T00:00:00Z", filingUrl: "https://evil.invalid" };
    expect(confirmedPutFields(confirmed)).not.toHaveProperty("id");
    expect(confirmedPutFields(confirmed)).not.toHaveProperty("updatedAt");
    expect(confirmedPutFields(confirmed)).not.toHaveProperty("filingUrl");
    expect(confirmedPutFields(confirmed)).toMatchObject({ lockVersion: 0, siteName: "qiuxs" });
  });

  it("recognizes only the documented settings conflict", () => {
    expect(isSettingsConflict(new ApiProblem(409, "settings_conflict", "r1", "Conflict"))).toBe(true);
    expect(isSettingsConflict(new ApiProblem(409, "release_conflict", "r2", "Conflict"))).toBe(false);
    expect(isSettingsConflict(new ApiProblem(503, "settings_conflict", "r3", "Conflict"))).toBe(false);
  });
});
