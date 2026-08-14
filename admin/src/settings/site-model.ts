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
      if (url.protocol !== "https:" || url.username || url.password || url.port || /\s/u.test(link.url) || url.hostname.length === 0) errors.push("socialLinks");
    } catch { errors.push("socialLinks"); }
  }
  if (draft.seoDefaultImageMediaId !== null && (!Number.isSafeInteger(draft.seoDefaultImageMediaId) || draft.seoDefaultImageMediaId < 1)) errors.push("seoDefaultImageMediaId");
  const request = { ...draft } as Record<string, unknown>;
  delete request.id; delete request.updatedAt; delete request.filingUrl;
  if (bytes(JSON.stringify(request)) > MiB) errors.push("request");
  return [...new Set(errors)];
}

export function buildPutRequest(draft: SiteDraft): PutSiteSettingsRequest {
  const errors = validateSiteDraft(draft);
  if (errors.length > 0) throw new Error(`Invalid site settings: ${errors.join(", ")}`);
  return {
    lockVersion: draft.lockVersion,
    siteName: draft.siteName,
    authorName: draft.authorName,
    authorBio: draft.authorBio,
    homeStatus: draft.homeStatus,
    aboutMd: draft.aboutMd,
    socialLinks: draft.socialLinks.map((link) => ({ ...link })),
    seoDefaultTitle: draft.seoDefaultTitle,
    seoDefaultDescription: draft.seoDefaultDescription,
    seoDefaultImageMediaId: draft.seoDefaultImageMediaId,
    filingName: draft.filingName,
    filingNumber: draft.filingNumber,
  };
}

export function siteDraftFromView(view: SiteSettingsView): SiteDraft {
  return { id: view.id, lockVersion: view.lockVersion, siteName: view.siteName, authorName: view.authorName, authorBio: view.authorBio, homeStatus: view.homeStatus, aboutMd: view.aboutMd, socialLinks: view.socialLinks.map((link) => ({ ...link })), seoDefaultTitle: view.seoDefaultTitle, seoDefaultDescription: view.seoDefaultDescription, seoDefaultImageMediaId: view.seoDefaultImageMediaId, filingName: view.filingName, filingNumber: view.filingNumber, filingUrl: view.filingUrl, updatedAt: view.updatedAt };
}
