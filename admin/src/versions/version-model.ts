import type { ArticleDetail, DraftView, LockVersionRequest, RevisionView, VersionResult } from "../api/admin-api";
import { requireSafeInteger } from "../api/ids";
import type { EditorDocument } from "../editor/editor-document";
import type { SaveState } from "../editor/useAutosave";

export type RevisionRow = RevisionView;

export interface VersionCapability {
  kind: SaveState["kind"];
  lockVersion: number;
  title: string;
  contentMd: string;
}

export function versionCapabilityFromAutosave(state: SaveState, document: EditorDocument): VersionCapability {
  return { kind: state.kind, lockVersion: state.lockVersion, title: document.title, contentMd: document.contentMd };
}

export function createVersionRequest(lockVersion: number): LockVersionRequest {
  return { lockVersion: requireSafeInteger(lockVersion, "lockVersion", 1) };
}

export function restoreVersionRequest(lockVersion: number): LockVersionRequest {
  return createVersionRequest(lockVersion);
}

export function canCreateVersion(capability: VersionCapability, currentLockVersion?: number): boolean {
  return capability.kind === "saved"
    && capability.title.trim().length > 0
    && !/blob:/iu.test(capability.contentMd)
    && (currentLockVersion === undefined || capability.lockVersion === currentLockVersion);
}

export function restoreVersionDecision(confirmed: boolean, revisionId: number): number | undefined {
  return confirmed ? requireSafeInteger(revisionId, "revision.id", 1) : undefined;
}

export function mapRevisionRows(items: readonly RevisionView[]): RevisionRow[] {
  return items.map((item) => ({
    ...item,
    id: requireSafeInteger(item.id, "revision.id", 1),
    articleId: requireSafeInteger(item.articleId, "revision.articleId", 1),
    tags: item.tags.map((tag) => ({ ...tag })),
    media: item.media.map((media) => ({ ...media })),
  }));
}

export function reasonLabel(reason: RevisionView["reason"]): string {
  return reason === "manual_version" ? "Manual version" : "Publish snapshot";
}

export function versionResultDraft(result: VersionResult): DraftView {
  return result.draft;
}

export function replaceArticleDraft(article: ArticleDetail, draft: DraftView): ArticleDetail {
  return {
    ...article,
    draftRevisionId: requireSafeInteger(draft.id, "draft.id", 1),
    updatedAt: draft.updatedAt,
    draft,
  };
}
