import type { components } from "./generated/admin";
import { ApiProblem, invalidApiResponse } from "./problem";

type Schema<Name extends keyof components["schemas"]> = components["schemas"][Name];

type CreateReleaseRequest = Schema<"CreateReleaseRequest">;
type LockVersionRequest = Schema<"LockVersionRequest">;
type PutSiteSettingsRequest = Schema<"PutSiteSettingsRequest">;
type RegisterMediaRequest = Schema<"RegisterMediaRequest">;
type SaveDraftRequest = Schema<"SaveDraftRequest">;

export type EntityId = number;

function invalid(): never {
  throw invalidApiResponse();
}

export function requireSafeInteger(value: unknown, _field: string, minimum: number): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < minimum) {
    return invalid();
  }
  return value;
}

function requireNullableSafeInteger(value: unknown, field: string, minimum: number): number | null {
  return requireNullable(value, field, (candidate) => requireSafeInteger(candidate, field, minimum));
}

export function requireEntityId(value: unknown, field: string): EntityId {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < 1) {
    throw new ApiProblem(502, "invalid_api_response", "client", `Invalid ${field}`);
  }
  return value;
}

function requireRecord(value: unknown, _field: string): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return invalid();
  }
  return value as Record<string, unknown>;
}

function requireField(record: Record<string, unknown>, key: string): unknown {
  if (!Object.prototype.hasOwnProperty.call(record, key) || record[key] === undefined) {
    return invalid();
  }
  return record[key];
}

function requireString(value: unknown, _field: string): string {
  if (typeof value !== "string") {
    return invalid();
  }
  return value;
}

function requireBoolean(value: unknown, _field: string): boolean {
  if (typeof value !== "boolean") {
    return invalid();
  }
  return value;
}

function requireEnum<const Value extends string>(
  value: unknown,
  _field: string,
  allowed: readonly Value[],
): Value {
  if (typeof value !== "string" || !allowed.includes(value as Value)) {
    return invalid();
  }
  return value as Value;
}

function requireArray(
  value: unknown,
  field: string,
  validateItem: (item: unknown, field: string) => void,
  bounds: { minimum?: number; maximum?: number } = {},
): unknown[] {
  if (
    !Array.isArray(value)
    || (bounds.minimum !== undefined && value.length < bounds.minimum)
    || (bounds.maximum !== undefined && value.length > bounds.maximum)
  ) {
    return invalid();
  }
  value.forEach((item, index) => validateItem(item, `${field}[${index}]`));
  return value;
}

function requireNullable<Result>(
  value: unknown,
  _field: string,
  validateValue: (value: unknown) => Result,
): Result | null {
  return value === null ? null : validateValue(value);
}

function stringField(record: Record<string, unknown>, key: string, field: string): void {
  requireString(requireField(record, key), `${field}.${key}`);
}

function integerField(record: Record<string, unknown>, key: string, field: string, minimum: number): void {
  requireSafeInteger(requireField(record, key), `${field}.${key}`, minimum);
}

function nullableIntegerField(record: Record<string, unknown>, key: string, field: string, minimum: number): void {
  requireNullableSafeInteger(requireField(record, key), `${field}.${key}`, minimum);
}

function nullableStringField(record: Record<string, unknown>, key: string, field: string): void {
  requireNullable(requireField(record, key), `${field}.${key}`, (value) => requireString(value, `${field}.${key}`));
}

export function validateSaveDraftRequest(input: SaveDraftRequest): void {
  requireSafeInteger(input.lockVersion, "lockVersion", 1);
  requireNullableSafeInteger(input.coverMediaId, "coverMediaId", 1);
  input.tagIds.forEach((tagId, index) => requireSafeInteger(tagId, `tagIds[${index}]`, 1));
}

export function validateLockVersionRequest(input: LockVersionRequest): void {
  requireSafeInteger(input.lockVersion, "lockVersion", 1);
}

export function validateRegisterMediaRequest(input: RegisterMediaRequest): void {
  requireSafeInteger(input.gfsFileId, "gfsFileId", 1);
}

export function validatePutSiteSettingsRequest(input: PutSiteSettingsRequest): void {
  requireSafeInteger(input.lockVersion, "lockVersion", 0);
  requireNullableSafeInteger(input.seoDefaultImageMediaId, "seoDefaultImageMediaId", 1);
}

export function validateCreateReleaseRequest(input: CreateReleaseRequest): void {
  requireNullableSafeInteger(input.articleId, "articleId", 1);
}

function validateTagSnapshot(value: unknown, field: string): void {
  const tag = requireRecord(value, field);
  integerField(tag, "tagId", field, 1);
  stringField(tag, "name", field);
  stringField(tag, "slug", field);
  integerField(tag, "position", field, 0);
}

function validateMediaReference(value: unknown, field: string): void {
  const media = requireRecord(value, field);
  integerField(media, "mediaId", field, 1);
  stringField(media, "publicKey", field);
  requireEnum(requireField(media, "purpose"), `${field}.purpose`, ["cover", "content"]);
  integerField(media, "position", field, 0);
}

function validateArticleRevision(
  value: unknown,
  field: string,
  status: "editing" | "frozen",
  reasons: readonly ("draft" | "manual_version" | "publish_snapshot")[],
): void {
  const revision = requireRecord(value, field);
  integerField(revision, "id", field, 1);
  integerField(revision, "articleId", field, 1);
  integerField(revision, "revisionNo", field, 1);
  integerField(revision, "lockVersion", field, 1);
  requireEnum(requireField(revision, "status"), `${field}.status`, [status]);
  requireEnum(requireField(revision, "reason"), `${field}.reason`, reasons);
  stringField(revision, "title", field);
  stringField(revision, "summary", field);
  nullableIntegerField(revision, "coverMediaId", field, 1);
  stringField(revision, "contentMd", field);
  stringField(revision, "contentHash", field);
  requireArray(requireField(revision, "tags"), `${field}.tags`, validateTagSnapshot, { maximum: 32 });
  requireArray(requireField(revision, "media"), `${field}.media`, validateMediaReference, { maximum: 257 });
  stringField(revision, "createdAt", field);
  stringField(revision, "updatedAt", field);
}

function validateDraft(value: unknown, field: string): void {
  validateArticleRevision(value, field, "editing", ["draft"]);
}

function validateRevision(value: unknown, field: string): void {
  validateArticleRevision(value, field, "frozen", ["manual_version", "publish_snapshot"]);
}

function validateArticleSummary(value: unknown, field: string): void {
  const article = requireRecord(value, field);
  integerField(article, "id", field, 1);
  stringField(article, "slug", field);
  integerField(article, "draftRevisionId", field, 1);
  nullableIntegerField(article, "publishedRevisionId", field, 1);
  requireEnum(requireField(article, "state"), `${field}.state`, ["active", "trashed"]);
  stringField(article, "draftTitle", field);
  stringField(article, "draftUpdatedAt", field);
  stringField(article, "createdAt", field);
  stringField(article, "updatedAt", field);
}

function validateTag(value: unknown, field: string): void {
  const tag = requireRecord(value, field);
  integerField(tag, "id", field, 1);
  stringField(tag, "name", field);
  stringField(tag, "slug", field);
  stringField(tag, "createdAt", field);
  stringField(tag, "updatedAt", field);
}

function validateBuilderTarget(value: unknown, field: string): void {
  const target = requireRecord(value, field);
  stringField(target, "name", field);
  stringField(target, "baseUrl", field);
  stringField(target, "username", field);
  stringField(target, "jobName", field);
}

function validatePublishJob(value: unknown, field: string): void {
  const job = requireRecord(value, field);
  integerField(job, "id", field, 1);
  integerField(job, "releaseId", field, 1);
  integerField(job, "builderId", field, 1);
  validateBuilderTarget(requireField(job, "builderTarget"), `${field}.builderTarget`);
  requireEnum(requireField(job, "status"), `${field}.status`, [
    "pending", "queued", "building", "deploying", "success", "failed",
  ]);
  stringField(job, "stage", field);
  nullableIntegerField(job, "buildNumber", field, 1);
  stringField(job, "errorSummary", field);
  stringField(job, "createdAt", field);
  nullableStringField(job, "finishedAt", field);
}

function validateRelease(value: unknown, field: string): void {
  const release = requireRecord(value, field);
  integerField(release, "id", field, 1);
  requireEnum(requireField(release, "status"), `${field}.status`, ["queued", "success", "failed"]);
  stringField(release, "checksum", field);
  stringField(release, "createdAt", field);
  nullableStringField(release, "completedAt", field);
  validatePublishJob(requireField(release, "latestJob"), `${field}.latestJob`);
  requireArray(requireField(release, "jobs"), `${field}.jobs`, validatePublishJob, { minimum: 1 });
}

export function validateAdminView(value: unknown): void {
  const admin = requireRecord(value, "response");
  integerField(admin, "id", "response", 1);
  stringField(admin, "username", "response");
}

export function validateArticleList(value: unknown): void {
  const list = requireRecord(value, "response");
  requireArray(requireField(list, "items"), "response.items", validateArticleSummary);
}

export function validateArticleDetail(value: unknown): void {
  const article = requireRecord(value, "response");
  integerField(article, "id", "response", 1);
  stringField(article, "slug", "response");
  integerField(article, "draftRevisionId", "response", 1);
  nullableIntegerField(article, "publishedRevisionId", "response", 1);
  requireEnum(requireField(article, "state"), "response.state", ["active", "trashed"]);
  stringField(article, "createdAt", "response");
  stringField(article, "updatedAt", "response");
  validateDraft(requireField(article, "draft"), "response.draft");
}

export function validateDraftView(value: unknown): void {
  validateDraft(value, "response");
}

export function validatePreviewView(value: unknown): void {
  const preview = requireRecord(value, "response");
  stringField(preview, "slug", "response");
  validateDraft(requireField(preview, "draft"), "response.draft");
}

export function validateRevisionList(value: unknown): void {
  const list = requireRecord(value, "response");
  requireArray(requireField(list, "items"), "response.items", validateRevision);
}

export function validateVersionResult(value: unknown): void {
  const result = requireRecord(value, "response");
  validateRevision(requireField(result, "version"), "response.version");
  validateDraft(requireField(result, "draft"), "response.draft");
}

export function validateTagView(value: unknown): void {
  validateTag(value, "response");
}

export function validateTagList(value: unknown): void {
  const list = requireRecord(value, "response");
  requireArray(requireField(list, "items"), "response.items", validateTag);
}

export function validateMediaView(value: unknown): void {
  const media = requireRecord(value, "response");
  integerField(media, "id", "response", 1);
  stringField(media, "publicKey", "response");
  integerField(media, "gfsFileId", "response", 1);
  stringField(media, "originalName", "response");
  requireEnum(requireField(media, "mimeType"), "response.mimeType", [
    "image/jpeg", "image/png", "image/webp", "image/gif",
  ]);
  integerField(media, "fileSize", "response", 1);
  integerField(media, "width", "response", 1);
  integerField(media, "height", "response", 1);
  requireEnum(requireField(media, "state"), "response.state", ["active"]);
  stringField(media, "url", "response");
  stringField(media, "createdAt", "response");
  stringField(media, "updatedAt", "response");
}

function validateSocialLink(value: unknown, field: string): void {
  const link = requireRecord(value, field);
  stringField(link, "label", field);
  stringField(link, "url", field);
}

export function validateSiteSettingsView(value: unknown): void {
  const site = requireRecord(value, "response");
  nullableIntegerField(site, "id", "response", 1);
  integerField(site, "lockVersion", "response", 0);
  stringField(site, "siteName", "response");
  stringField(site, "authorName", "response");
  stringField(site, "authorBio", "response");
  stringField(site, "homeStatus", "response");
  stringField(site, "aboutMd", "response");
  requireArray(requireField(site, "socialLinks"), "response.socialLinks", validateSocialLink, { maximum: 16 });
  stringField(site, "seoDefaultTitle", "response");
  stringField(site, "seoDefaultDescription", "response");
  nullableIntegerField(site, "seoDefaultImageMediaId", "response", 1);
  stringField(site, "filingName", "response");
  stringField(site, "filingNumber", "response");
  stringField(site, "filingUrl", "response");
  nullableStringField(site, "updatedAt", "response");
}

export function validateBuilderConfigView(value: unknown): void {
  const builder = requireRecord(value, "response");
  integerField(builder, "id", "response", 1);
  stringField(builder, "name", "response");
  stringField(builder, "baseUrl", "response");
  stringField(builder, "username", "response");
  stringField(builder, "jobName", "response");
  requireBoolean(requireField(builder, "enabled"), "response.enabled");
  requireBoolean(requireField(builder, "tokenConfigured"), "response.tokenConfigured");
}

export function validateReleaseView(value: unknown): void {
  validateRelease(value, "response");
}

export function validateReleaseList(value: unknown): void {
  const list = requireRecord(value, "response");
  requireArray(requireField(list, "items"), "response.items", validateRelease);
}

export function validateCreateReleaseResult(value: unknown): void {
  const result = requireRecord(value, "response");
  validateRelease(requireField(result, "release"), "response.release");
  validatePublishJob(requireField(result, "job"), "response.job");
}

export function validateRetryReleaseResult(value: unknown): void {
  validateCreateReleaseResult(value);
}

export function validateMediaUploadPolicy(value: unknown): void {
  const policy = requireRecord(value, "response");
  for (const field of [
    "uploadUrl", "appId", "policy", "signature", "timestamp", "expire", "nonce", "fileField",
  ] as const) {
    stringField(policy, field, "response");
  }
}

function validateHotlinkEntry(value: unknown, field: string): void {
  const entry = requireRecord(value, field);
  stringField(entry, "hostname", field);
  requireBoolean(requireField(entry, "enabled"), `${field}.enabled`);
}

export function validateHotlinkSettingsView(value: unknown): void {
  const settings = requireRecord(value, "response");
  requireBoolean(requireField(settings, "allowEmptyReferer"), "response.allowEmptyReferer");
  requireArray(requireField(settings, "entries"), "response.entries", validateHotlinkEntry);
}
