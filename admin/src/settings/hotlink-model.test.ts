import { describe, expect, it } from "vitest";
import { ApiProblem } from "../api/problem";
import {
  applyHotlinkCache,
  buildHotlinkPutRequest,
  defaults,
  draftFromHotlinkView,
  hotlinkConflictState,
  normalizeHostname,
  validateHotlinkDraft,
  type HotlinkDraft,
} from "./hotlink-model";

const valid: HotlinkDraft = {
  allowEmptyReferer: true,
  entries: [
    { hostname: " qiuxs.com. ", enabled: true },
    { hostname: "blog-admin.qiuxs.com", enabled: false },
  ],
};

describe("hotlink settings pure model", () => {
  it("normalizes valid DNS hostnames and rejects unsafe or noncanonical values", () => {
    expect(normalizeHostname("  EXAMPLE.COM. ")).toBe("example.com");
    expect(normalizeHostname("localhost")).toBe("localhost");
    for (const value of [
      "", " ", ".", "example..com", "-example.com", "example-.com", "example.com/",
      "https://example.com", "example.com:443", "user@example.com", "*.example.com",
      "例子.测试", "1.2.3.4", "01.2.3.4", "123.456", "foo bar.com", "a\tb.com",
      "a".repeat(64) + ".com", "a".repeat(254),
    ]) expect(normalizeHostname(value), value).toBeUndefined();
    expect(normalizeHostname("a".repeat(63) + ".com")).toBe("a".repeat(63) + ".com");
  });

  it("provides exact defaults, normalizes entries and rejects duplicates", () => {
    expect(defaults).toEqual({ allowEmptyReferer: true, entries: [
      { hostname: "qiuxs.com", enabled: true },
      { hostname: "blog-admin.qiuxs.com", enabled: true },
    ] });
    expect(validateHotlinkDraft(valid)).toEqual([]);
    expect(buildHotlinkPutRequest(valid)).toEqual({ allowEmptyReferer: true, entries: [
      { hostname: "qiuxs.com", enabled: true },
      { hostname: "blog-admin.qiuxs.com", enabled: false },
    ] });
    expect(validateHotlinkDraft({ ...valid, entries: [{ hostname: "A.com", enabled: true }, { hostname: "a.com.", enabled: false }] })).toContain("entries");
    expect(buildHotlinkPutRequest({ ...valid, entries: [{ hostname: " A.COM. ", enabled: true }] })).toEqual({ allowEmptyReferer: true, entries: [{ hostname: "a.com", enabled: true }] });
  });

  it("retains the local draft on settings conflict and replaces cache immediately on success", () => {
    const local = { ...valid, allowEmptyReferer: false };
    const saved = { allowEmptyReferer: false, entries: [{ hostname: "example.com", enabled: true }] };
    expect(draftFromHotlinkView(saved)).toEqual(saved);
    expect(applyHotlinkCache(defaults, saved)).toBe(saved);
    const problem = new ApiProblem(409, "settings_conflict", "request-1", "Settings changed");
    expect(hotlinkConflictState(problem, local)).toEqual({ conflict: true, draft: local, message: "Hotlink settings changed on the server. Your local changes are preserved." });
    expect(hotlinkConflictState(new ApiProblem(503, "dependency_unavailable", "request-2", "Unavailable"), local)).toEqual({ conflict: false, draft: local, message: undefined });
  });
});
