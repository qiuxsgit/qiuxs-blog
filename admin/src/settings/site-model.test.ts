import { describe, expect, it } from "vitest";
import { defaults, buildPutRequest, isCanonicalSocialUrl, validateSiteDraft, type SiteDraft } from "./site-model";

const valid: SiteDraft = {
  ...defaults,
  siteName: "qiuxs",
  authorName: "qiuxs",
  authorBio: "",
  homeStatus: "",
  aboutMd: "# About",
  socialLinks: [{ label: "GitHub", url: "https://github.com/qiuxs" }],
  seoDefaultTitle: "",
  seoDefaultDescription: "",
  seoDefaultImageMediaId: null,
  filingName: "长安休息室",
  filingNumber: "浙ICP备17057726号-1",
};

describe("site settings pure model", () => {
  it("uses the documented virtual defaults and fixed filing URL", () => {
    expect(defaults).toMatchObject({ siteName: "qiuxs", filingName: "长安休息室", filingNumber: "浙ICP备17057726号-1", lockVersion: 0, id: null, updatedAt: null, filingUrl: "https://beian.miit.gov.cn/" });
  });

  it("builds the exact PUT body and omits readonly/cache fields", () => {
    expect(buildPutRequest(valid)).toEqual({ lockVersion: 0, siteName: "qiuxs", authorName: "qiuxs", authorBio: "", homeStatus: "", aboutMd: "# About", socialLinks: [{ label: "GitHub", url: "https://github.com/qiuxs" }], seoDefaultTitle: "", seoDefaultDescription: "", seoDefaultImageMediaId: null, filingName: "长安休息室", filingNumber: "浙ICP备17057726号-1" });
    expect(buildPutRequest(valid)).not.toHaveProperty("id");
    expect(buildPutRequest(valid)).not.toHaveProperty("updatedAt");
    expect(buildPutRequest(valid)).not.toHaveProperty("filingUrl");
  });

  it("counts rune limits and rejects blank filing fields", () => {
    expect(validateSiteDraft({ ...valid, siteName: "😀".repeat(100) })).toEqual([]);
    expect(validateSiteDraft({ ...valid, siteName: "😀".repeat(101) })).toContain("siteName");
    expect(validateSiteDraft({ ...valid, filingName: "  " })).toContain("filingName");
    expect(validateSiteDraft({ ...valid, filingNumber: "" })).toContain("filingNumber");
  });

  it("enforces Markdown bytes, encoded envelope bytes, and social URL rules", () => {
    expect(validateSiteDraft({ ...valid, aboutMd: "a".repeat(2 * 1024 * 1024) })).toContain("request");
    expect(validateSiteDraft({ ...valid, aboutMd: "a".repeat(2 * 1024 * 1024 + 1) })).toContain("aboutMd");
    expect(validateSiteDraft({ ...valid, socialLinks: [{ label: "GitHub", url: "http://github.com" }] })).toContain("socialLinks");
    expect(validateSiteDraft({ ...valid, socialLinks: [{ label: "GitHub", url: "https://github.com" }, { label: "github", url: "https://example.com" }] })).toContain("socialLinks");
  });

  it("matches the service canonical absolute HTTPS social URL rules", () => {
    const accepted = ["https://github.com", "https://github.com/path?q=1#part", "https://example.com/#?", "https://example.com/#foo?", "https://[2001:db8::1]/docs", "https://example.com:8443/a", "https://192.0.2.1/profile", "https://example.com/a%2Fb"];
    const rejected = ["HTTPS://github.com", "https://EXAMPLE.com", "https://user:pass@example.com", "https://example.com:443", "https://example.com.", "https://192.168.001.1", "https://123", "https://[2001:0db8::1]", "https://example.com/a/../b", "https://example.com/a%7eb", "https://example.com/a%2fb", "https://example.com/?q=%7e", "https://example.com/?q=["];
    for (const url of accepted) expect(isCanonicalSocialUrl(url), url).toBe(true);
    for (const url of rejected) expect(isCanonicalSocialUrl(url), url).toBe(false);
  });

  it("measures the final PUT JSON envelope rather than the draft object", () => {
    const nearLimit = "a".repeat(2 * 1024 * 1024 - 400);
    expect(validateSiteDraft({ ...valid, aboutMd: nearLimit })).not.toContain("request");
  });
});
