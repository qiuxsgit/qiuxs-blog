import type { ArticleDetail, DraftView, LockVersionRequest, RevisionView, VersionResult } from "../api/admin-api";
import { requireSafeInteger } from "../api/ids";
import type { SaveState } from "../editor/useAutosave";

export type RevisionRow = RevisionView;

export function createVersionRequest(lockVersion: number): LockVersionRequest {
  return { lockVersion: requireSafeInteger(lockVersion, "lockVersion", 1) };
}

export function restoreVersionRequest(lockVersion: number): LockVersionRequest {
  return createVersionRequest(lockVersion);
}

export function canCreateVersion(state: SaveState): boolean {
  return state.kind === "saved";
}

export function mapRevisionRows(items: readonly RevisionView[]): RevisionRow[] {
  return items.map((item) => ({
    ...item,
    id: requireSafeInteger(item.id, "revision.id", 1),
    articleId: requireSafeInteger(item.articleId, "revision.articleId", 1),
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
