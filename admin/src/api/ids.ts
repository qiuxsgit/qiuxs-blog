import type { components } from "./generated/admin";
import { ApiProblem, invalidApiResponse } from "./problem";

type Schema<Name extends keyof components["schemas"]> = components["schemas"][Name];

type AdminView = Schema<"AdminView">;
type ArticleDetail = Schema<"ArticleDetail">;
type ArticleList = Schema<"ArticleList">;
type BuilderConfigView = Schema<"BuilderConfigView">;
type CreateReleaseRequest = Schema<"CreateReleaseRequest">;
type CreateReleaseResult = Schema<"CreateReleaseResult">;
type DraftView = Schema<"DraftView">;
type HotlinkSettingsView = Schema<"HotlinkSettingsView">;
type LockVersionRequest = Schema<"LockVersionRequest">;
type MediaUploadPolicy = Schema<"MediaUploadPolicy">;
type MediaView = Schema<"MediaView">;
type PreviewView = Schema<"PreviewView">;
type PublishJobView = Schema<"PublishJobView">;
type PutSiteSettingsRequest = Schema<"PutSiteSettingsRequest">;
type RegisterMediaRequest = Schema<"RegisterMediaRequest">;
type ReleaseList = Schema<"ReleaseList">;
type ReleaseView = Schema<"ReleaseView">;
type RetryReleaseResult = Schema<"RetryReleaseResult">;
type RevisionList = Schema<"RevisionList">;
type SaveDraftRequest = Schema<"SaveDraftRequest">;
type SiteSettingsView = Schema<"SiteSettingsView">;
type TagList = Schema<"TagList">;
type TagView = Schema<"TagView">;
type VersionResult = Schema<"VersionResult">;

const adminViewKeys = ["id", "username"] as const satisfies readonly (keyof AdminView)[];
const articleSummaryKeys = ["id", "slug", "draftRevisionId", "publishedRevisionId", "state", "draftTitle", "draftUpdatedAt", "createdAt", "updatedAt"] as const satisfies readonly (keyof Schema<"ArticleSummary">)[];
const articleDetailKeys = ["id", "slug", "draftRevisionId", "publishedRevisionId", "state", "createdAt", "updatedAt", "draft"] as const satisfies readonly (keyof ArticleDetail)[];
const draftViewKeys = ["id", "articleId", "revisionNo", "lockVersion", "status", "reason", "title", "summary", "coverMediaId", "contentMd", "contentHash", "tags", "media", "createdAt", "updatedAt"] as const satisfies readonly (keyof DraftView)[];
const tagSnapshotKeys = ["tagId", "name", "slug", "position"] as const satisfies readonly (keyof Schema<"TagSnapshot">)[];
const mediaReferenceKeys = ["mediaId", "publicKey", "purpose", "position"] as const satisfies readonly (keyof Schema<"MediaReference">)[];
const previewViewKeys = ["slug", "draft"] as const satisfies readonly (keyof PreviewView)[];
const itemsKeys = ["items"] as const;
const versionResultKeys = ["version", "draft"] as const satisfies readonly (keyof VersionResult)[];
const tagViewKeys = ["id", "name", "slug", "createdAt", "updatedAt"] as const satisfies readonly (keyof TagView)[];
const mediaUploadPolicyKeys = ["uploadUrl", "appId", "policy", "signature", "timestamp", "expire", "nonce", "fileField"] as const satisfies readonly (keyof MediaUploadPolicy)[];
const mediaViewKeys = ["id", "publicKey", "gfsFileId", "originalName", "mimeType", "fileSize", "width", "height", "state", "url", "createdAt", "updatedAt"] as const satisfies readonly (keyof MediaView)[];
const siteSettingsKeys = ["id", "lockVersion", "siteName", "authorName", "authorBio", "homeStatus", "aboutMd", "socialLinks", "seoDefaultTitle", "seoDefaultDescription", "seoDefaultImageMediaId", "filingName", "filingNumber", "filingUrl", "updatedAt"] as const satisfies readonly (keyof SiteSettingsView)[];
const hotlinkSettingsKeys = ["allowEmptyReferer", "entries"] as const satisfies readonly (keyof HotlinkSettingsView)[];
const hotlinkEntryKeys = ["hostname", "enabled"] as const satisfies readonly (keyof Schema<"HotlinkEntry">)[];
const builderConfigKeys = ["id", "name", "baseUrl", "username", "jobName", "enabled", "tokenConfigured"] as const satisfies readonly (keyof BuilderConfigView)[];
const publishJobKeys = ["id", "releaseId", "builderId", "builderTarget", "status", "stage", "buildNumber", "errorSummary", "createdAt", "finishedAt"] as const satisfies readonly (keyof PublishJobView)[];
const builderTargetKeys = ["name", "baseUrl", "username", "jobName"] as const satisfies readonly (keyof Schema<"BuilderTargetView">)[];
const releaseKeys = ["id", "status", "checksum", "createdAt", "completedAt", "latestJob", "jobs"] as const satisfies readonly (keyof ReleaseView)[];
const releaseResultKeys = ["release", "job"] as const satisfies readonly (keyof CreateReleaseResult)[];

export type EntityId = number;

function invalidInteger(field: string): never {
  throw new ApiProblem(502, "invalid_api_response", "client", `Invalid ${field}`);
}

export function requireSafeInteger(value: unknown, field: string, minimum: number): number {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value < minimum) {
    return invalidInteger(field);
  }
  return value;
}

function requireNullableSafeInteger(value: unknown, field: string, minimum: number): number | null {
  return value === null ? null : requireSafeInteger(value, field, minimum);
}

export function requireEntityId(value: unknown, field: string): EntityId {
  return requireSafeInteger(value, field, 1);
}

function requireRecord(value: unknown, field: string): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new ApiProblem(502, "invalid_api_response", "client", `Invalid ${field}`);
  }
  return value as Record<string, unknown>;
}

function requireArray(value: unknown, field: string): unknown[] {
  if (!Array.isArray(value)) {
    throw new ApiProblem(502, "invalid_api_response", "client", `Invalid ${field}`);
  }
  return value;
}

function requireKeys<T extends object>(
  value: unknown,
  field: string,
  keys: readonly (keyof T)[],
): Record<string, unknown> {
  const record = requireRecord(value, field);
  for (const key of keys) {
    if (typeof key !== "string" || !(key in record) || record[key] === undefined) {
      throw invalidApiResponse();
    }
  }
  return record;
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
  const tag = requireKeys<Schema<"TagSnapshot">>(value, field, tagSnapshotKeys);
  requireSafeInteger(tag.tagId, `${field}.tagId`, 1);
}

function validateMediaReference(value: unknown, field: string): void {
  const media = requireKeys<Schema<"MediaReference">>(value, field, mediaReferenceKeys);
  requireSafeInteger(media.mediaId, `${field}.mediaId`, 1);
}

function validateDraft(value: unknown, field: string): void {
  const draft = requireKeys<DraftView>(value, field, draftViewKeys);
  requireSafeInteger(draft.id, `${field}.id`, 1);
  requireSafeInteger(draft.articleId, `${field}.articleId`, 1);
  requireSafeInteger(draft.revisionNo, `${field}.revisionNo`, 1);
  requireSafeInteger(draft.lockVersion, `${field}.lockVersion`, 1);
  requireNullableSafeInteger(draft.coverMediaId, `${field}.coverMediaId`, 1);
  requireArray(draft.tags, `${field}.tags`).forEach((tag, index) => validateTagSnapshot(tag, `${field}.tags[${index}]`));
  requireArray(draft.media, `${field}.media`).forEach((media, index) => validateMediaReference(media, `${field}.media[${index}]`));
}

function validateArticleSummary(value: unknown, field: string): void {
  const article = requireKeys<Schema<"ArticleSummary">>(value, field, articleSummaryKeys);
  requireSafeInteger(article.id, `${field}.id`, 1);
  requireSafeInteger(article.draftRevisionId, `${field}.draftRevisionId`, 1);
  requireNullableSafeInteger(article.publishedRevisionId, `${field}.publishedRevisionId`, 1);
}

function validatePublishJob(value: unknown, field: string): void {
  const job = requireKeys<PublishJobView>(value, field, publishJobKeys);
  requireSafeInteger(job.id, `${field}.id`, 1);
  requireSafeInteger(job.releaseId, `${field}.releaseId`, 1);
  requireSafeInteger(job.builderId, `${field}.builderId`, 1);
  requireNullableSafeInteger(job.buildNumber, `${field}.buildNumber`, 1);
  requireKeys<Schema<"BuilderTargetView">>(job.builderTarget, `${field}.builderTarget`, builderTargetKeys);
}

function validateRelease(value: unknown, field: string): void {
  const release = requireKeys<ReleaseView>(value, field, releaseKeys);
  requireSafeInteger(release.id, `${field}.id`, 1);
  validatePublishJob(release.latestJob, `${field}.latestJob`);
  requireArray(release.jobs, `${field}.jobs`).forEach((job, index) => validatePublishJob(job, `${field}.jobs[${index}]`));
}

export function validateAdminView(value: AdminView): void {
  const admin = requireKeys<AdminView>(value, "response", adminViewKeys);
  requireSafeInteger(admin.id, "response.id", 1);
}

export function validateArticleList(value: ArticleList): void {
  const list = requireKeys<ArticleList>(value, "response", itemsKeys);
  requireArray(list.items, "response.items").forEach((item, index) => validateArticleSummary(item, `response.items[${index}]`));
}

export function validateArticleDetail(value: ArticleDetail): void {
  const article = requireKeys<ArticleDetail>(value, "response", articleDetailKeys);
  requireSafeInteger(article.id, "response.id", 1);
  requireSafeInteger(article.draftRevisionId, "response.draftRevisionId", 1);
  requireNullableSafeInteger(article.publishedRevisionId, "response.publishedRevisionId", 1);
  validateDraft(article.draft, "response.draft");
}

export function validateDraftView(value: DraftView): void {
  validateDraft(value, "response");
}

export function validatePreviewView(value: PreviewView): void {
  const preview = requireKeys<PreviewView>(value, "response", previewViewKeys);
  validateDraft(preview.draft, "response.draft");
}

export function validateRevisionList(value: RevisionList): void {
  const list = requireKeys<RevisionList>(value, "response", itemsKeys);
  requireArray(list.items, "response.items").forEach((item, index) => validateDraft(item, `response.items[${index}]`));
}

export function validateVersionResult(value: VersionResult): void {
  const result = requireKeys<VersionResult>(value, "response", versionResultKeys);
  validateDraft(result.version, "response.version");
  validateDraft(result.draft, "response.draft");
}

export function validateTagView(value: TagView): void {
  const tag = requireKeys<TagView>(value, "response", tagViewKeys);
  requireSafeInteger(tag.id, "response.id", 1);
}

export function validateTagList(value: TagList): void {
  const list = requireKeys<TagList>(value, "response", itemsKeys);
  requireArray(list.items, "response.items").forEach((item, index) => {
    const tag = requireKeys<TagView>(item, `response.items[${index}]`, tagViewKeys);
    requireSafeInteger(tag.id, `response.items[${index}].id`, 1);
  });
}

export function validateMediaView(value: MediaView): void {
  const media = requireKeys<MediaView>(value, "response", mediaViewKeys);
  requireSafeInteger(media.id, "response.id", 1);
  requireSafeInteger(media.gfsFileId, "response.gfsFileId", 1);
  requireSafeInteger(media.fileSize, "response.fileSize", 1);
}

export function validateSiteSettingsView(value: SiteSettingsView): void {
  const site = requireKeys<SiteSettingsView>(value, "response", siteSettingsKeys);
  requireNullableSafeInteger(site.id, "response.id", 1);
  requireSafeInteger(site.lockVersion, "response.lockVersion", 0);
  requireNullableSafeInteger(site.seoDefaultImageMediaId, "response.seoDefaultImageMediaId", 1);
}

export function validateBuilderConfigView(value: BuilderConfigView): void {
  const builder = requireKeys<BuilderConfigView>(value, "response", builderConfigKeys);
  requireSafeInteger(builder.id, "response.id", 1);
}

export function validateReleaseView(value: ReleaseView): void {
  validateRelease(value, "response");
}

export function validateReleaseList(value: ReleaseList): void {
  const list = requireKeys<ReleaseList>(value, "response", itemsKeys);
  requireArray(list.items, "response.items").forEach((item, index) => validateRelease(item, `response.items[${index}]`));
}

export function validateCreateReleaseResult(value: CreateReleaseResult): void {
  const result = requireKeys<CreateReleaseResult>(value, "response", releaseResultKeys);
  validateRelease(result.release, "response.release");
  validatePublishJob(result.job, "response.job");
}

export function validateRetryReleaseResult(value: RetryReleaseResult): void {
  validateCreateReleaseResult(value);
}

export function validateMediaUploadPolicy(value: MediaUploadPolicy): void {
  requireKeys<MediaUploadPolicy>(value, "response", mediaUploadPolicyKeys);
}

export function validateHotlinkSettingsView(value: HotlinkSettingsView): void {
  const settings = requireKeys<HotlinkSettingsView>(value, "response", hotlinkSettingsKeys);
  requireArray(settings.entries, "response.entries").forEach((entry, index) => {
    requireKeys<Schema<"HotlinkEntry">>(entry, `response.entries[${index}]`, hotlinkEntryKeys);
  });
}
