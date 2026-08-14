import type { PutSiteSettingsRequest, SiteSettingsView } from "../api/admin-api";

export type SiteDraft = Pick<SiteSettingsView, "lockVersion" | "siteName" | "authorName" | "authorBio" | "homeStatus" | "aboutMd" | "socialLinks" | "seoDefaultTitle" | "seoDefaultDescription" | "seoDefaultImageMediaId" | "filingName" | "filingNumber"> & Partial<Pick<SiteSettingsView, "id" | "updatedAt" | "filingUrl">>;

export const defaults: SiteDraft = {
  id: null,
  lockVersion: 0,
  siteName: "qiuxs",
  authorName: "qiuxs",
  authorBio: "",
  homeStatus: "",
  aboutMd: "",
  socialLinks: [],
  seoDefaultTitle: "",
  seoDefaultDescription: "",
  seoDefaultImageMediaId: null,
  filingName: "长安休息室",
  filingNumber: "浙ICP备17057726号-1",
  filingUrl: "https://beian.miit.gov.cn/",
  updatedAt: null,
};

const MiB = 2 * 1024 * 1024;
const limits = { siteName: 100, authorName: 100, authorBio: 1000, homeStatus: 500, seoDefaultTitle: 100, seoDefaultDescription: 300, filingName: 100, filingNumber: 100 } as const;
const bytes = (value: string) => new TextEncoder().encode(value).byteLength;
const runes = (value: string) => Array.from(value).length;

function canonicalComponent(raw: string): string | undefined {
  let result = "";
  for (let index = 0; index < raw.length; index += 1) {
    const current = raw[index] ?? "";
    if (current === "%") {
      const encoded = raw.slice(index + 1, index + 3);
      if (!/^[0-9A-Fa-f]{2}$/u.test(encoded)) return undefined;
      const value = Number.parseInt(encoded, 16);
      if (value >= 0x21 && value <= 0x7e && /[A-Za-z0-9\-._~]/u.test(String.fromCharCode(value))) result += String.fromCharCode(value);
      else result += `%${encoded.toUpperCase()}`;
      index += 2;
      continue;
    }
    if (current.charCodeAt(0) < 0x21 || current.charCodeAt(0) > 0x7e) return undefined;
    result += current;
  }
  return result;
}

/** Mirrors service/internal/settings canonicalSocialURL. */
export function isCanonicalSocialUrl(raw: string): boolean {
  if (!raw || raw !== raw.trim() || !raw.startsWith("https://")) return false;
  let parsed: URL;
  try { parsed = new URL(raw); } catch { return false; }
  if (parsed.protocol !== "https:" || parsed.username || parsed.password || parsed.hostname === "") return false;
  const authority = raw.slice("https://".length).split(/[/?#]/u, 1)[0];
  if (authority !== parsed.host || authority.includes("@")) return false;
  const port = authority.match(/:(\d+)$/u)?.[1];
  if (port !== undefined && (port === "443" || Number.parseInt(port, 10).toString() !== port)) return false;
  const hostname = parsed.hostname;
  if (hostname.endsWith(".") || /^(?:[0-9]+\.)*[0-9]+$/u.test(hostname)) return false;
  if (hostname.includes(":")) {
    if (authority !== parsed.host) return false;
  } else if (hostname !== hostname.toLowerCase() || /[^a-z0-9.-]/u.test(hostname)) return false;
  const path = raw.slice("https://".length + authority.length).split(/[?#]/u, 1)[0] ?? "";
  if (path && path !== parsed.pathname) return false;
  if (path.split("/").some((segment) => segment === "." || segment === "..")) return false;
  const queryStart = raw.indexOf("?");
  const fragmentStart = raw.indexOf("#");
  const query = queryStart >= 0 ? raw.slice(queryStart + 1, fragmentStart >= 0 ? fragmentStart : undefined) : "";
  const fragment = fragmentStart >= 0 ? raw.slice(fragmentStart + 1) : "";
  if (queryStart >= 0 && (!query || canonicalComponent(query) !== query)) return false;
  if (fragmentStart >= 0 && (!fragment || canonicalComponent(fragment) !== fragment)) return false;
  return true;
}

function putRequestFromDraft(draft: SiteDraft): PutSiteSettingsRequest {
  return {
    lockVersion: draft.lockVersion, siteName: draft.siteName, authorName: draft.authorName, authorBio: draft.authorBio,
    homeStatus: draft.homeStatus, aboutMd: draft.aboutMd, socialLinks: draft.socialLinks.map((link) => ({ ...link })),
    seoDefaultTitle: draft.seoDefaultTitle, seoDefaultDescription: draft.seoDefaultDescription,
    seoDefaultImageMediaId: draft.seoDefaultImageMediaId, filingName: draft.filingName, filingNumber: draft.filingNumber,
  };
}

export function validateSiteDraft(draft: SiteDraft): string[] {
  const errors: string[] = [];
  for (const [field, limit] of Object.entries(limits) as [keyof typeof limits, number][]) {
    if (runes(draft[field]) > limit) errors.push(field);
  }
  if (draft.filingName.trim() === "") errors.push("filingName");
  if (draft.filingNumber.trim() === "") errors.push("filingNumber");
  if (bytes(draft.aboutMd) > MiB) errors.push("aboutMd");
  if (draft.socialLinks.length > 16) errors.push("socialLinks");
  const labels = new Set<string>();
  for (const link of draft.socialLinks) {
    const label = link.label.trim().toLocaleLowerCase();
    if (!label || labels.has(label)) errors.push("socialLinks");
    labels.add(label);
    try {
      const url = new URL(link.url);
      if (!isCanonicalSocialUrl(link.url)) errors.push("socialLinks");
    } catch { errors.push("socialLinks"); }
  }
  if (draft.seoDefaultImageMediaId !== null && (!Number.isSafeInteger(draft.seoDefaultImageMediaId) || draft.seoDefaultImageMediaId < 1)) errors.push("seoDefaultImageMediaId");
  if (bytes(JSON.stringify(putRequestFromDraft(draft))) > MiB) errors.push("request");
  return [...new Set(errors)];
}

export function buildPutRequest(draft: SiteDraft): PutSiteSettingsRequest {
  const errors = validateSiteDraft(draft);
  if (errors.length > 0) throw new Error(`Invalid site settings: ${errors.join(", ")}`);
  return putRequestFromDraft(draft);
}

export function siteDraftFromView(view: SiteSettingsView): SiteDraft {
  return { id: view.id, lockVersion: view.lockVersion, siteName: view.siteName, authorName: view.authorName, authorBio: view.authorBio, homeStatus: view.homeStatus, aboutMd: view.aboutMd, socialLinks: view.socialLinks.map((link) => ({ ...link })), seoDefaultTitle: view.seoDefaultTitle, seoDefaultDescription: view.seoDefaultDescription, seoDefaultImageMediaId: view.seoDefaultImageMediaId, filingName: view.filingName, filingNumber: view.filingNumber, filingUrl: view.filingUrl, updatedAt: view.updatedAt };
}
