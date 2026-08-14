import type { ArticleDetail, SaveDraftRequest } from "../api/admin-api";
import type { EntityId } from "../api/ids";

export const MAX_DOCUMENT_BYTES = 2 * 1024 * 1024;
export const MAX_SELECTED_TAGS = 32;

export interface EditorDocument {
  title: string;
  summary: string;
  coverMediaId: EntityId | null;
  contentMd: string;
  tagIds: EntityId[];
}

export interface EditorValidationOptions {
  rejectBlobUrls?: boolean;
}

const utf8 = new TextEncoder();

function codePointLength(value: string): number {
  return Array.from(value).length;
}

function positiveInteger(value: number): boolean {
  return Number.isSafeInteger(value) && value > 0;
}

function uniquePositiveIds(ids: readonly EntityId[]): boolean {
  return ids.every(positiveInteger) && new Set(ids).size === ids.length;
}

function unsafeSaveRequest(document: EditorDocument, lockVersion: number): SaveDraftRequest {
  return {
    lockVersion,
    title: document.title,
    summary: document.summary,
    coverMediaId: document.coverMediaId,
    contentMd: document.contentMd,
    tagIds: [...document.tagIds],
  };
}

export function fromArticleDetail(article: ArticleDetail): EditorDocument {
  const seen = new Set<EntityId>();
  const tagIds = [...article.draft.tags]
    .sort((left, right) => left.position - right.position)
    .flatMap(({ tagId }) => {
      if (seen.has(tagId)) return [];
      seen.add(tagId);
      return [tagId];
    });

  return {
    title: article.draft.title,
    summary: article.draft.summary,
    coverMediaId: article.draft.coverMediaId,
    contentMd: article.draft.contentMd,
    tagIds,
  };
}

export function validateTagName(name: string): string | undefined {
  const length = codePointLength(name);
  return length >= 1 && length <= 64 ? undefined : "Tag name must contain 1–64 characters.";
}

export function validateEditorDocument(
  document: EditorDocument,
  lockVersion: number,
  options: EditorValidationOptions = {},
): string[] {
  const errors: string[] = [];
  if (!positiveInteger(lockVersion)) errors.push("Lock version must be a positive integer.");
  if (codePointLength(document.title) > 200) errors.push("Title must be at most 200 characters.");
  if (codePointLength(document.summary) > 600) errors.push("Summary must be at most 600 characters.");
  if (document.coverMediaId !== null && !positiveInteger(document.coverMediaId)) {
    errors.push("Cover media ID must be a positive integer or empty.");
  }
  if (!uniquePositiveIds(document.tagIds)) errors.push("Tag IDs must be positive, ordered, and unique.");
  if (document.tagIds.length > MAX_SELECTED_TAGS) errors.push("Select at most 32 tags.");
  if (utf8.encode(document.contentMd).byteLength >= MAX_DOCUMENT_BYTES) {
    errors.push("Markdown must be smaller than 2 MiB.");
  }
  if (utf8.encode(JSON.stringify(unsafeSaveRequest(document, lockVersion))).byteLength >= MAX_DOCUMENT_BYTES) {
    errors.push("Draft request must be smaller than 2 MiB.");
  }
  if (options.rejectBlobUrls && /(?:^|[('"\s])blob:/iu.test(document.contentMd)) {
    errors.push("Upload local images before versioning or publishing.");
  }
  return errors;
}

export function toSaveRequest(document: EditorDocument, lockVersion: number): SaveDraftRequest {
  const errors = validateEditorDocument(document, lockVersion);
  if (errors.length > 0) throw new Error(errors[0]);
  return unsafeSaveRequest(document, lockVersion);
}

export function toggleTagId(
  selected: readonly EntityId[],
  tagId: EntityId,
  checked = !selected.includes(tagId),
): EntityId[] {
  if (!positiveInteger(tagId)) return [...selected];
  const unique = selected.filter((id, index) => selected.indexOf(id) === index);
  if (!checked) return unique.filter((id) => id !== tagId);
  if (unique.includes(tagId) || unique.length >= MAX_SELECTED_TAGS) return unique;
  return [...unique, tagId];
}
