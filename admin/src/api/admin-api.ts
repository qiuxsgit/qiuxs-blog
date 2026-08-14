import createClient, { type Client } from "openapi-fetch";

import type { components, operations, paths } from "./generated/admin";
import { requireEntityId, requireResponseEntityIds, type EntityId } from "./ids";
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
  loginAdmin(input: LoginRequest): Promise<AdminView>;
  logoutAdmin(): Promise<void>;
  getCurrentAdmin(): Promise<AdminView>;
  listArticles(query?: ListArticlesQuery): Promise<ArticleList>;
  createArticle(): Promise<ArticleDetail>;
  getArticle(articleId: EntityId): Promise<ArticleDetail>;
  saveArticleDraft(articleId: EntityId, input: SaveDraftRequest): Promise<DraftView>;
  getArticlePreview(articleId: EntityId): Promise<PreviewView>;
  listArticleVersions(articleId: EntityId): Promise<RevisionList>;
  createArticleVersion(articleId: EntityId, input: LockVersionRequest): Promise<VersionResult>;
  restoreArticleVersion(articleId: EntityId, revisionId: EntityId, input: LockVersionRequest): Promise<DraftView>;
  trashArticle(articleId: EntityId): Promise<void>;
  untrashArticle(articleId: EntityId): Promise<void>;
  listTags(): Promise<TagList>;
  createTag(input: CreateTagRequest): Promise<TagView>;
  renameTag(tagId: EntityId, input: RenameTagRequest): Promise<TagView>;
  createMediaUploadPolicy(): Promise<MediaUploadPolicy>;
  registerMedia(input: RegisterMediaRequest): Promise<MediaView>;
  getSiteSettings(): Promise<SiteSettingsView>;
  putSiteSettings(input: PutSiteSettingsRequest): Promise<SiteSettingsView>;
  getHotlinkSettings(): Promise<HotlinkSettingsView>;
  putHotlinkSettings(input: PutHotlinkSettingsRequest): Promise<HotlinkSettingsView>;
  getBuilderConfig(): Promise<BuilderConfigView>;
  putBuilderConfig(input: PutBuilderConfigRequest): Promise<BuilderConfigView>;
  testBuilderConfig(): Promise<void>;
  listReleases(query?: ListReleasesQuery): Promise<ReleaseList>;
  createRelease(input: CreateReleaseRequest): Promise<CreateReleaseResult>;
  getRelease(releaseId: EntityId): Promise<ReleaseView>;
  retryRelease(releaseId: EntityId): Promise<RetryReleaseResult>;
}

interface WireResult {
  data?: unknown;
  error?: unknown;
  response: Response;
}

async function execute(call: () => Promise<WireResult>): Promise<WireResult> {
  try {
    return await call();
  } catch {
    throw networkProblem();
  }
}

function responseProblem(result: WireResult, options: AdminApiOptions): ApiProblem | undefined {
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
    result.error.code,
    result.error.requestId,
    "Request failed",
    result.error.type,
  );
}

function unwrap<T>(result: WireResult, status: number, options: AdminApiOptions): T {
  if (result.response.status === status && result.data !== undefined) {
    requireResponseEntityIds(result.data);
    return result.data as T;
  }
  const problem = responseProblem(result, options);
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
    async loginAdmin(input) {
      return unwrap<AdminView>(await execute(() => client.POST("/api/admin/v1/session", { body: input })), 200, options);
    },
    async logoutAdmin() {
      return unwrapVoid(await execute(() => client.DELETE("/api/admin/v1/session")), 204, options);
    },
    async getCurrentAdmin() {
      return unwrap<AdminView>(await execute(() => client.GET("/api/admin/v1/me")), 200, options);
    },
    async listArticles(query = {}) {
      return unwrap<ArticleList>(await execute(() => client.GET("/api/admin/v1/articles", { params: { query } })), 200, options);
    },
    async createArticle() {
      return unwrap<ArticleDetail>(await execute(() => client.POST("/api/admin/v1/articles")), 201, options);
    },
    async getArticle(articleId) {
      const safeArticleId = requireEntityId(articleId, "articleId");
      return unwrap<ArticleDetail>(await execute(() => client.GET("/api/admin/v1/articles/{articleId}", {
        params: { path: { articleId: safeArticleId } },
      })), 200, options);
    },
    async saveArticleDraft(articleId, input) {
      const safeArticleId = requireEntityId(articleId, "articleId");
      return unwrap<DraftView>(await execute(() => client.PUT("/api/admin/v1/articles/{articleId}/draft", {
        params: { path: { articleId: safeArticleId } },
        body: input,
      })), 200, options);
    },
    async getArticlePreview(articleId) {
      const safeArticleId = requireEntityId(articleId, "articleId");
      return unwrap<PreviewView>(await execute(() => client.GET("/api/admin/v1/articles/{articleId}/preview", {
        params: { path: { articleId: safeArticleId } },
      })), 200, options);
    },
    async listArticleVersions(articleId) {
      const safeArticleId = requireEntityId(articleId, "articleId");
      return unwrap<RevisionList>(await execute(() => client.GET("/api/admin/v1/articles/{articleId}/versions", {
        params: { path: { articleId: safeArticleId } },
      })), 200, options);
    },
    async createArticleVersion(articleId, input) {
      const safeArticleId = requireEntityId(articleId, "articleId");
      return unwrap<VersionResult>(await execute(() => client.POST("/api/admin/v1/articles/{articleId}/versions", {
        params: { path: { articleId: safeArticleId } },
        body: input,
      })), 201, options);
    },
    async restoreArticleVersion(articleId, revisionId, input) {
      const safeArticleId = requireEntityId(articleId, "articleId");
      const safeRevisionId = requireEntityId(revisionId, "revisionId");
      return unwrap<DraftView>(await execute(() => client.POST("/api/admin/v1/articles/{articleId}/versions/{revisionId}/restore", {
        params: { path: {
          articleId: safeArticleId,
          revisionId: safeRevisionId,
        } },
        body: input,
      })), 200, options);
    },
    async trashArticle(articleId) {
      const safeArticleId = requireEntityId(articleId, "articleId");
      return unwrapVoid(await execute(() => client.POST("/api/admin/v1/articles/{articleId}/trash", {
        params: { path: { articleId: safeArticleId } },
      })), 204, options);
    },
    async untrashArticle(articleId) {
      const safeArticleId = requireEntityId(articleId, "articleId");
      return unwrapVoid(await execute(() => client.POST("/api/admin/v1/articles/{articleId}/untrash", {
        params: { path: { articleId: safeArticleId } },
      })), 204, options);
    },
    async listTags() {
      return unwrap<TagList>(await execute(() => client.GET("/api/admin/v1/tags")), 200, options);
    },
    async createTag(input) {
      return unwrap<TagView>(await execute(() => client.POST("/api/admin/v1/tags", { body: input })), 201, options);
    },
    async renameTag(tagId, input) {
      const safeTagId = requireEntityId(tagId, "tagId");
      return unwrap<TagView>(await execute(() => client.PATCH("/api/admin/v1/tags/{tagId}", {
        params: { path: { tagId: safeTagId } },
        body: input,
      })), 200, options);
    },
    async createMediaUploadPolicy() {
      return unwrap<MediaUploadPolicy>(await execute(() => client.POST("/api/admin/v1/media/upload-policy")), 200, options);
    },
    async registerMedia(input) {
      return unwrap<MediaView>(await execute(() => client.POST("/api/admin/v1/media", { body: input })), 201, options);
    },
    async getSiteSettings() {
      return unwrap<SiteSettingsView>(await execute(() => client.GET("/api/admin/v1/settings/site")), 200, options);
    },
    async putSiteSettings(input) {
      return unwrap<SiteSettingsView>(await execute(() => client.PUT("/api/admin/v1/settings/site", { body: input })), 200, options);
    },
    async getHotlinkSettings() {
      return unwrap<HotlinkSettingsView>(await execute(() => client.GET("/api/admin/v1/settings/hotlink")), 200, options);
    },
    async putHotlinkSettings(input) {
      return unwrap<HotlinkSettingsView>(await execute(() => client.PUT("/api/admin/v1/settings/hotlink", { body: input })), 200, options);
    },
    async getBuilderConfig() {
      return unwrap<BuilderConfigView>(await execute(() => client.GET("/api/admin/v1/builder")), 200, options);
    },
    async putBuilderConfig(input) {
      return unwrap<BuilderConfigView>(await execute(() => client.PUT("/api/admin/v1/builder", { body: input })), 200, options);
    },
    async testBuilderConfig() {
      return unwrapVoid(await execute(() => client.POST("/api/admin/v1/builder/test")), 204, options);
    },
    async listReleases(query = {}) {
      return unwrap<ReleaseList>(await execute(() => client.GET("/api/admin/v1/releases", { params: { query } })), 200, options);
    },
    async createRelease(input) {
      return unwrap<CreateReleaseResult>(await execute(() => client.POST("/api/admin/v1/releases", { body: input })), 202, options);
    },
    async getRelease(releaseId) {
      const safeReleaseId = requireEntityId(releaseId, "releaseId");
      return unwrap<ReleaseView>(await execute(() => client.GET("/api/admin/v1/releases/{releaseId}", {
        params: { path: { releaseId: safeReleaseId } },
      })), 200, options);
    },
    async retryRelease(releaseId) {
      const safeReleaseId = requireEntityId(releaseId, "releaseId");
      return unwrap<RetryReleaseResult>(await execute(() => client.POST("/api/admin/v1/releases/{releaseId}/retry", {
        params: { path: { releaseId: safeReleaseId } },
      })), 202, options);
    },
  } satisfies AdminApi;
}

export function createAdminApi(options: AdminApiOptions = {}): AdminApi {
  const client = createClient<paths>({
    baseUrl: window.location.origin,
    credentials: "include",
    fetch: options.fetch ?? globalThis.fetch,
    headers: { Accept: "application/json, application/problem+json" },
  });
  return buildAdminApi(client, options) satisfies AdminApi;
}
