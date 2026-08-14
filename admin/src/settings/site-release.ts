import type { CreateReleaseRequest } from "../api/admin-api";
import type { SiteDraft } from "./site-model";

export function conflictLocalDraft(local: SiteDraft, _confirmed: SiteDraft): SiteDraft { return { ...local, socialLinks: local.socialLinks.map((link) => ({ ...link })) }; }

/** Copies only fields that can be sent in the PUT body; readonly metadata never enters a conflict draft. */
export function confirmedPutFields(confirmed: SiteDraft): SiteDraft {
  return {
    lockVersion: confirmed.lockVersion,
    siteName: confirmed.siteName,
    authorName: confirmed.authorName,
    authorBio: confirmed.authorBio,
    homeStatus: confirmed.homeStatus,
    aboutMd: confirmed.aboutMd,
    socialLinks: confirmed.socialLinks.map((link) => ({ ...link })),
    seoDefaultTitle: confirmed.seoDefaultTitle,
    seoDefaultDescription: confirmed.seoDefaultDescription,
    seoDefaultImageMediaId: confirmed.seoDefaultImageMediaId,
    filingName: confirmed.filingName,
    filingNumber: confirmed.filingNumber,
  };
}

export function publishSettingsRequest(): CreateReleaseRequest { return { mode: "publish_settings", articleId: null }; }
