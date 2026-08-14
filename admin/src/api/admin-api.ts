import createClient, { type Client } from "openapi-fetch";

import type { components, operations, paths } from "./generated/admin";
import {
  requireEntityId,
  validateAdminView,
  validateArticleDetail,
  validateArticleList,
  validateBuilderConfigView,
  validateCreateReleaseRequest,
  validateCreateReleaseResult,
  validateDraftView,
  validateLockVersionRequest,
  validateHotlinkSettingsView,
  validateMediaUploadPolicy,
  validateMediaView,
  validatePreviewView,
  validatePutSiteSettingsRequest,
  validateRegisterMediaRequest,
  validateReleaseList,
  validateReleaseView,
  validateRetryReleaseResult,
  validateRevisionList,
  validateSaveDraftRequest,
  validateSiteSettingsView,
  validateTagList,
  validateTagView,
  validateVersionResult,
  type EntityId,
} from "./ids";
import { ApiProblem, invalidApiResponse, isProblem, networkProblem } from "./problem";

type Schema<Name extends keyof components["schemas"]> = components["schemas"][Name];

export type LoginRequest = Schema<"LoginRequest">;
export type AdminView = Schema<"AdminView">;
export type ArticleSummary = Schema<"ArticleSummary">;
export type ArticleList = Schema<"ArticleList">;
export type ArticleDetail = Schema<"ArticleDetail">;
export type DraftView = Schema<"DraftView">;
export type PreviewView = Schema<"PreviewView">;
export type RevisionView = Schema<"RevisionView">;
export type RevisionList = Schema<"RevisionList">;
export type VersionResult = Schema<"VersionResult">;
export type SaveDraftRequest = Schema<"SaveDraftRequest">;
export type LockVersionRequest = Schema<"LockVersionRequest">;
export type TagView = Schema<"TagView">;
export type TagList = Schema<"TagList">;
export type CreateTagRequest = Schema<"CreateTagRequest">;
export type RenameTagRequest = Schema<"RenameTagRequest">;
export type MediaUploadPolicy = Schema<"MediaUploadPolicy">;
export type MediaView = Schema<"MediaView">;
export type RegisterMediaRequest = Schema<"RegisterMediaRequest">;
export type SiteSettingsView = Schema<"SiteSettingsView">;
export type PutSiteSettingsRequest = Schema<"PutSiteSettingsRequest">;
export type HotlinkSettingsView = Schema<"HotlinkSettingsView">;
export type PutHotlinkSettingsRequest = Schema<"PutHotlinkSettingsRequest">;
export type BuilderConfigView = Schema<"BuilderConfigView">;
export type PutBuilderConfigRequest = Schema<"PutBuilderConfigRequest">;
export type CreateReleaseRequest = Schema<"CreateReleaseRequest">;
export type ReleaseView = Schema<"ReleaseView">;
export type CreateReleaseResult = Schema<"CreateReleaseResult">;
export type RetryReleaseResult = Schema<"RetryReleaseResult">;
export type ReleaseList = Schema<"ReleaseList">;
export type PublishJobView = Schema<"PublishJobView">;
export type Problem = Schema<"Problem">;
export type ListArticlesQuery = NonNullable<operations["listArticles"]["parameters"]["query"]>;
export type ListReleasesQuery = NonNullable<operations["listReleases"]["parameters"]["query"]>;

export { ApiProblem } from "./problem";
export type { EntityId } from "./ids";

export interface AdminApiOptions {
  fetch?: (input: Request) => Promise<Response>;
  onUnauthenticated?: () => void;
}

export interface AdminApi {
  loginAdmin(input: LoginRequest, signal?: AbortSignal): Promise<AdminView>;
  logoutAdmin(signal?: AbortSignal): Promise<void>;
  getCurrentAdmin(signal?: AbortSignal): Promise<AdminView>;
  listArticles(query?: ListArticlesQuery, signal?: AbortSignal): Promise<ArticleList>;
  createArticle(signal?: AbortSignal): Promise<ArticleDetail>;
  getArticle(articleId: EntityId, signal?: AbortSignal): Promise<ArticleDetail>;
  saveArticleDraft(articleId: EntityId, input: SaveDraftRequest, signal?: AbortSignal): Promise<DraftView>;
  getArticlePreview(articleId: EntityId, signal?: AbortSignal): Promise<PreviewView>;
  listArticleVersions(articleId: EntityId, signal?: AbortSignal): Promise<RevisionList>;
  createArticleVersion(articleId: EntityId, input: LockVersionRequest, signal?: AbortSignal): Promise<VersionResult>;
  restoreArticleVersion(articleId: EntityId, revisionId: EntityId, input: LockVersionRequest, signal?: AbortSignal): Promise<DraftView>;
  trashArticle(articleId: EntityId, signal?: AbortSignal): Promise<void>;
  untrashArticle(articleId: EntityId, signal?: AbortSignal): Promise<void>;
  listTags(signal?: AbortSignal): Promise<TagList>;
  createTag(input: CreateTagRequest, signal?: AbortSignal): Promise<TagView>;
  renameTag(tagId: EntityId, input: RenameTagRequest, signal?: AbortSignal): Promise<TagView>;
  createMediaUploadPolicy(signal?: AbortSignal): Promise<MediaUploadPolicy>;
  registerMedia(input: RegisterMediaRequest, signal?: AbortSignal): Promise<MediaView>;
  getSiteSettings(signal?: AbortSignal): Promise<SiteSettingsView>;
  putSiteSettings(input: PutSiteSettingsRequest, signal?: AbortSignal): Promise<SiteSettingsView>;
  getHotlinkSettings(signal?: AbortSignal): Promise<HotlinkSettingsView>;
  putHotlinkSettings(input: PutHotlinkSettingsRequest, signal?: AbortSignal): Promise<HotlinkSettingsView>;
  getBuilderConfig(signal?: AbortSignal): Promise<BuilderConfigView>;
  putBuilderConfig(input: PutBuilderConfigRequest, signal?: AbortSignal): Promise<BuilderConfigView>;
  testBuilderConfig(signal?: AbortSignal): Promise<void>;
  listReleases(query?: ListReleasesQuery, signal?: AbortSignal): Promise<ReleaseList>;
  createRelease(input: CreateReleaseRequest, signal?: AbortSignal): Promise<CreateReleaseResult>;
  getRelease(releaseId: EntityId, signal?: AbortSignal): Promise<ReleaseView>;
  retryRelease(releaseId: EntityId, signal?: AbortSignal): Promise<RetryReleaseResult>;
}

interface WireResult {
  data?: unknown;
  error?: unknown;
  response: Response;
}

class FetchFailure extends Error {
  constructor() {
    super("Fetch failed");
    this.name = "FetchFailure";
  }
}

function isAbortError(error: unknown): boolean {
  return typeof error === "object" && error !== null && "name" in error && error.name === "AbortError";
}

function tagFetchFailures(fetch: (input: Request) => Promise<Response>): (input: Request) => Promise<Response> {
  return async (request) => {
    try {
      return await fetch(request);
    } catch (error) {
      if (isAbortError(error)) {
        throw error;
      }
      throw new FetchFailure();
    }
  };
}

function withSignal(signal?: AbortSignal): { signal?: AbortSignal } {
  return signal === undefined ? {} : { signal };
}

async function execute(call: () => Promise<WireResult>): Promise<WireResult> {
  try {
    return await call();
  } catch (error) {
    if (isAbortError(error)) {
      throw error;
    }
    if (error instanceof FetchFailure) {
      throw networkProblem();
    }
    throw invalidApiResponse();
  }
}

function containsSecret(value: string, sensitiveValues: readonly string[]): boolean {
  return sensitiveValues.some((secret) => secret.length > 0 && value.includes(secret));
}

function sanitizedField(value: string, sensitiveValues: readonly string[], fallback: string): string {
  return containsSecret(value, sensitiveValues) ? fallback : value;
}

function responseProblem(
  result: WireResult,
  options: AdminApiOptions,
  sensitiveValues: readonly string[] = [],
): ApiProblem | undefined {
  const contentType = result.response.headers.get("Content-Type")?.split(";", 1)[0]?.trim().toLowerCase();
  if (contentType !== "application/problem+json" || !isProblem(result.error)) {
    return undefined;
  }
  if (result.error.status !== result.response.status) {
    return undefined;
  }
  if (result.error.status === 401 && result.error.code === "unauthenticated") {
    options.onUnauthenticated?.();
  }
  return new ApiProblem(
    result.error.status,
    sanitizedField(result.error.code, sensitiveValues, "redacted"),
    sanitizedField(result.error.requestId, sensitiveValues, "redacted"),
    sanitizedField(result.error.title, sensitiveValues, "Request failed"),
    sanitizedField(result.error.type, sensitiveValues, "about:blank"),
  );
}

function unwrap<T>(
  result: WireResult,
  status: number,
  options: AdminApiOptions,
  validate: (value: T) => void,
  sensitiveValues: readonly string[] = [],
): T {
  const contentType = result.response.headers.get("Content-Type")?.split(";", 1)[0]?.trim().toLowerCase();
  if (
    result.response.status === status
    && contentType === "application/json"
    && typeof result.data === "object"
    && result.data !== null
    && !Array.isArray(result.data)
  ) {
    const data = result.data as T;
    validate(data);
    return data;
  }
  const problem = responseProblem(result, options, sensitiveValues);
  if (problem) {
    throw problem;
  }
  throw invalidApiResponse();
}

function unwrapVoid(result: WireResult, status: number, options: AdminApiOptions): void {
  if (result.response.status === status && result.data === undefined && result.error === undefined) {
    return;
  }
  const problem = responseProblem(result, options);
  if (problem) {
    throw problem;
  }
  throw invalidApiResponse();
}

function buildAdminApi(client: Client<paths>, options: AdminApiOptions): AdminApi {
  return {
    async loginAdmin(input, signal) {
      return unwrap<AdminView>(await execute(() => client.POST("/api/admin/v1/session", { body: input, ...withSignal(signal) })), 200, options, validateAdminView, [input.password]);
    },
    async logoutAdmin(signal) {
      return unwrapVoid(await execute(() => client.DELETE("/api/admin/v1/session", { ...withSignal(signal) })), 204, options);
    },
    async getCurrentAdmin(signal) {
      return unwrap<AdminView>(await execute(() => client.GET("/api/admin/v1/me", { ...withSignal(signal) })), 200, options, validateAdminView);
    },
    async listArticles(query = {}, signal) {
      return unwrap<ArticleList>(await execute(() => client.GET("/api/admin/v1/articles", { params: { query }, ...withSignal(signal) })), 200, options, validateArticleList);
    },
    async createArticle(signal) {
      return unwrap<ArticleDetail>(await execute(() => client.POST("/api/admin/v1/articles", { ...withSignal(signal) })), 201, options, validateArticleDetail);
    },
    async getArticle(articleId, signal) {
      const safeArticleId = requireEntityId(articleId, "articleId");
      return unwrap<ArticleDetail>(await execute(() => client.GET("/api/admin/v1/articles/{articleId}", {
        params: { path: { articleId: safeArticleId } },
        ...withSignal(signal),
      })), 200, options, validateArticleDetail);
    },
    async saveArticleDraft(articleId, input, signal) {
      const safeArticleId = requireEntityId(articleId, "articleId");
      validateSaveDraftRequest(input);
      return unwrap<DraftView>(await execute(() => client.PUT("/api/admin/v1/articles/{articleId}/draft", {
        params: { path: { articleId: safeArticleId } },
        body: input,
        ...withSignal(signal),
      })), 200, options, validateDraftView);
    },
    async getArticlePreview(articleId, signal) {
      const safeArticleId = requireEntityId(articleId, "articleId");
      return unwrap<PreviewView>(await execute(() => client.GET("/api/admin/v1/articles/{articleId}/preview", {
        params: { path: { articleId: safeArticleId } },
        ...withSignal(signal),
      })), 200, options, validatePreviewView);
    },
    async listArticleVersions(articleId, signal) {
      const safeArticleId = requireEntityId(articleId, "articleId");
      return unwrap<RevisionList>(await execute(() => client.GET("/api/admin/v1/articles/{articleId}/versions", {
        params: { path: { articleId: safeArticleId } },
        ...withSignal(signal),
      })), 200, options, validateRevisionList);
    },
    async createArticleVersion(articleId, input, signal) {
      const safeArticleId = requireEntityId(articleId, "articleId");
      validateLockVersionRequest(input);
      return unwrap<VersionResult>(await execute(() => client.POST("/api/admin/v1/articles/{articleId}/versions", {
        params: { path: { articleId: safeArticleId } },
        body: input,
        ...withSignal(signal),
      })), 201, options, validateVersionResult);
    },
    async restoreArticleVersion(articleId, revisionId, input, signal) {
      const safeArticleId = requireEntityId(articleId, "articleId");
      const safeRevisionId = requireEntityId(revisionId, "revisionId");
      validateLockVersionRequest(input);
      return unwrap<DraftView>(await execute(() => client.POST("/api/admin/v1/articles/{articleId}/versions/{revisionId}/restore", {
        params: { path: {
          articleId: safeArticleId,
          revisionId: safeRevisionId,
        } },
        body: input,
        ...withSignal(signal),
      })), 200, options, validateDraftView);
    },
    async trashArticle(articleId, signal) {
      const safeArticleId = requireEntityId(articleId, "articleId");
      return unwrapVoid(await execute(() => client.POST("/api/admin/v1/articles/{articleId}/trash", {
        params: { path: { articleId: safeArticleId } },
        ...withSignal(signal),
      })), 204, options);
    },
    async untrashArticle(articleId, signal) {
      const safeArticleId = requireEntityId(articleId, "articleId");
      return unwrapVoid(await execute(() => client.POST("/api/admin/v1/articles/{articleId}/untrash", {
        params: { path: { articleId: safeArticleId } },
        ...withSignal(signal),
      })), 204, options);
    },
    async listTags(signal) {
      return unwrap<TagList>(await execute(() => client.GET("/api/admin/v1/tags", { ...withSignal(signal) })), 200, options, validateTagList);
    },
    async createTag(input, signal) {
      return unwrap<TagView>(await execute(() => client.POST("/api/admin/v1/tags", { body: input, ...withSignal(signal) })), 201, options, validateTagView);
    },
    async renameTag(tagId, input, signal) {
      const safeTagId = requireEntityId(tagId, "tagId");
      return unwrap<TagView>(await execute(() => client.PATCH("/api/admin/v1/tags/{tagId}", {
        params: { path: { tagId: safeTagId } },
        body: input,
        ...withSignal(signal),
      })), 200, options, validateTagView);
    },
    async createMediaUploadPolicy(signal) {
      return unwrap<MediaUploadPolicy>(await execute(() => client.POST("/api/admin/v1/media/upload-policy", { ...withSignal(signal) })), 200, options, validateMediaUploadPolicy);
    },
    async registerMedia(input, signal) {
      validateRegisterMediaRequest(input);
      return unwrap<MediaView>(await execute(() => client.POST("/api/admin/v1/media", { body: input, ...withSignal(signal) })), 201, options, validateMediaView);
    },
    async getSiteSettings(signal) {
      return unwrap<SiteSettingsView>(await execute(() => client.GET("/api/admin/v1/settings/site", { ...withSignal(signal) })), 200, options, validateSiteSettingsView);
    },
    async putSiteSettings(input, signal) {
      validatePutSiteSettingsRequest(input);
      return unwrap<SiteSettingsView>(await execute(() => client.PUT("/api/admin/v1/settings/site", { body: input, ...withSignal(signal) })), 200, options, validateSiteSettingsView);
    },
    async getHotlinkSettings(signal) {
      return unwrap<HotlinkSettingsView>(await execute(() => client.GET("/api/admin/v1/settings/hotlink", { ...withSignal(signal) })), 200, options, validateHotlinkSettingsView);
    },
    async putHotlinkSettings(input, signal) {
      return unwrap<HotlinkSettingsView>(await execute(() => client.PUT("/api/admin/v1/settings/hotlink", { body: input, ...withSignal(signal) })), 200, options, validateHotlinkSettingsView);
    },
    async getBuilderConfig(signal) {
      return unwrap<BuilderConfigView>(await execute(() => client.GET("/api/admin/v1/builder", { ...withSignal(signal) })), 200, options, validateBuilderConfigView);
    },
    async putBuilderConfig(input, signal) {
      const sensitiveValues = input.token === undefined ? [] : [input.token];
      return unwrap<BuilderConfigView>(await execute(() => client.PUT("/api/admin/v1/builder", { body: input, ...withSignal(signal) })), 200, options, validateBuilderConfigView, sensitiveValues);
    },
    async testBuilderConfig(signal) {
      return unwrapVoid(await execute(() => client.POST("/api/admin/v1/builder/test", { ...withSignal(signal) })), 204, options);
    },
    async listReleases(query = {}, signal) {
      return unwrap<ReleaseList>(await execute(() => client.GET("/api/admin/v1/releases", { params: { query }, ...withSignal(signal) })), 200, options, validateReleaseList);
    },
    async createRelease(input, signal) {
      validateCreateReleaseRequest(input);
      return unwrap<CreateReleaseResult>(await execute(() => client.POST("/api/admin/v1/releases", { body: input, ...withSignal(signal) })), 202, options, validateCreateReleaseResult);
    },
    async getRelease(releaseId, signal) {
      const safeReleaseId = requireEntityId(releaseId, "releaseId");
      return unwrap<ReleaseView>(await execute(() => client.GET("/api/admin/v1/releases/{releaseId}", {
        params: { path: { releaseId: safeReleaseId } },
        ...withSignal(signal),
      })), 200, options, validateReleaseView);
    },
    async retryRelease(releaseId, signal) {
      const safeReleaseId = requireEntityId(releaseId, "releaseId");
      return unwrap<RetryReleaseResult>(await execute(() => client.POST("/api/admin/v1/releases/{releaseId}/retry", {
        params: { path: { releaseId: safeReleaseId } },
        ...withSignal(signal),
      })), 202, options, validateRetryReleaseResult);
    },
  } satisfies AdminApi;
}

export function createAdminApi(options: AdminApiOptions = {}): AdminApi {
  const fetch = tagFetchFailures(options.fetch ?? globalThis.fetch);
  const client = createClient<paths>({
    baseUrl: window.location.origin,
    credentials: "include",
    fetch,
    headers: { Accept: "application/json, application/problem+json" },
  });
  return buildAdminApi(client, options) satisfies AdminApi;
}
