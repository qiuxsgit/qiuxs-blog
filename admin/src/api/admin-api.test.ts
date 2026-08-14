import { describe, expect, it, vi } from "vitest";

import {
  ApiProblem,
  createAdminApi,
  type AdminApi,
  type ArticleSummary,
  type CreateReleaseRequest,
  type LockVersionRequest,
  type LoginRequest,
  type PutBuilderConfigRequest,
  type PutHotlinkSettingsRequest,
  type PutSiteSettingsRequest,
  type RegisterMediaRequest,
  type SaveDraftRequest,
} from "./admin-api";
import { requireEntityId } from "./ids";
import { queryKeys } from "./query-keys";
import {
  articleDetail,
  articleList,
  articleSummary,
  builderConfig,
  draftView,
  failedJob,
  failedRelease,
  hotlinkSettings,
  mediaPolicy,
  mediaView,
  previewView,
  releaseList,
  revisionList,
  siteSettings,
  tagList,
  tagView,
  versionResult,
} from "../test/fixtures";

interface RequestRecord {
  method: string;
  pathname: string;
  search: string;
  body: string | null;
}

interface PlannedResponse {
  status: number;
  body?: unknown;
  contentType?: string;
}

function createRecordingFetch(planned: PlannedResponse[]) {
  const records: RequestRecord[] = [];
  const fetch: typeof globalThis.fetch = async (input, init) => {
    const request = new Request(input, init);
    const url = new URL(request.url);
    const text = request.method === "GET" || request.method === "HEAD"
      ? ""
      : await request.clone().text();
    records.push({
      method: request.method,
      pathname: url.pathname,
      search: url.search,
      body: text === "" ? null : text,
    });

    const next = planned.shift();
    if (!next) {
      throw new Error("Test response queue exhausted");
    }
    const body = next.body === undefined ? null : JSON.stringify(next.body);
    const responseInit: ResponseInit = { status: next.status };
    if (body !== null) {
      responseInit.headers = { "Content-Type": next.contentType ?? "application/json" };
    }
    return new Response(body, responseInit);
  };
  return { fetch, records };
}

const json = (status: number, body: unknown): PlannedResponse => ({ status, body });
const empty = (status: number): PlannedResponse => ({ status });

describe("createAdminApi", () => {
  it("maps every AdminApi operation to the exact wire method, path, query, body, and success status", async () => {
    const planned = [
      json(200, { id: 1, username: "admin" }),
      empty(204),
      json(200, { id: 1, username: "admin" }),
      json(200, articleList),
      json(201, articleDetail),
      json(200, articleDetail),
      json(200, draftView),
      json(200, previewView),
      json(200, revisionList),
      json(201, versionResult),
      json(200, draftView),
      empty(204),
      empty(204),
      json(200, tagList),
      json(201, tagView),
      json(200, tagView),
      json(200, mediaPolicy),
      json(201, mediaView),
      json(200, siteSettings),
      json(200, siteSettings),
      json(200, hotlinkSettings),
      json(200, hotlinkSettings),
      json(200, builderConfig),
      json(200, builderConfig),
      empty(204),
      json(200, releaseList),
      json(202, { release: failedRelease, job: failedJob }),
      json(200, failedRelease),
      json(202, { release: failedRelease, job: failedJob }),
    ];
    const recording = createRecordingFetch(planned);
    const api = createAdminApi({ fetch: recording.fetch }) satisfies AdminApi;

    const article = {
      id: 11,
      slug: "abc123_def45",
      draftRevisionId: 21,
      publishedRevisionId: null,
      state: "active",
      draftTitle: "Draft",
      draftUpdatedAt: "2026-08-14T00:00:00Z",
      createdAt: "2026-08-13T00:00:00Z",
      updatedAt: "2026-08-14T00:00:00Z",
    } satisfies ArticleSummary;
    expect(article.id).toBe(11);

    const login: LoginRequest = { username: "admin", password: "correct horse" };
    const saveDraft: SaveDraftRequest = {
      lockVersion: 7,
      title: "Build log",
      summary: "Summary",
      coverMediaId: null,
      contentMd: "# Build log\n",
      tagIds: [31],
    };
    const lockVersion: LockVersionRequest = { lockVersion: 7 };
    const registerMedia: RegisterMediaRequest = { gfsFileId: 41, originalName: "photo.png" };
    const putSite: PutSiteSettingsRequest = {
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
    };
    const putHotlink: PutHotlinkSettingsRequest = hotlinkSettings;
    const putBuilder: PutBuilderConfigRequest = {
      name: "home-jenkins",
      baseUrl: "https://jenkins.example.com",
      username: "blog-builder",
      token: "builder-token",
      jobName: "qiuxs-blog-site",
      enabled: true,
    };
    const createRelease: CreateReleaseRequest = { mode: "publish_article", articleId: 11 };

    expect(await api.loginAdmin(login)).toEqual({ id: 1, username: "admin" });
    expect(await api.logoutAdmin()).toBeUndefined();
    expect(await api.getCurrentAdmin()).toEqual({ id: 1, username: "admin" });
    expect(await api.listArticles({ state: "trashed" })).toEqual(articleList);
    expect(await api.createArticle()).toEqual(articleDetail);
    expect(await api.getArticle(11)).toEqual(articleDetail);
    expect(await api.saveArticleDraft(11, saveDraft)).toEqual(draftView);
    expect(await api.getArticlePreview(11)).toEqual(previewView);
    expect(await api.listArticleVersions(11)).toEqual(revisionList);
    expect(await api.createArticleVersion(11, lockVersion)).toEqual(versionResult);
    expect(await api.restoreArticleVersion(11, 41, lockVersion)).toEqual(draftView);
    expect(await api.trashArticle(11)).toBeUndefined();
    expect(await api.untrashArticle(11)).toBeUndefined();
    expect(await api.listTags()).toEqual(tagList);
    expect(await api.createTag({ name: "Go" })).toEqual(tagView);
    expect(await api.renameTag(31, { name: "Go" })).toEqual(tagView);
    expect(await api.createMediaUploadPolicy()).toEqual(mediaPolicy);
    expect(await api.registerMedia(registerMedia)).toEqual(mediaView);
    expect(await api.getSiteSettings()).toEqual(siteSettings);
    expect(await api.putSiteSettings(putSite)).toEqual(siteSettings);
    expect(await api.getHotlinkSettings()).toEqual(hotlinkSettings);
    expect(await api.putHotlinkSettings(putHotlink)).toEqual(hotlinkSettings);
    expect(await api.getBuilderConfig()).toEqual(builderConfig);
    expect(await api.putBuilderConfig(putBuilder)).toEqual(builderConfig);
    expect(await api.testBuilderConfig()).toBeUndefined();
    expect(await api.listReleases({ limit: 20, offset: 0 })).toEqual(releaseList);
    expect(await api.createRelease(createRelease)).toEqual({ release: failedRelease, job: failedJob });
    expect(await api.getRelease(71)).toEqual(failedRelease);
    expect(await api.retryRelease(71)).toEqual({ release: failedRelease, job: failedJob });

    expect(planned).toHaveLength(0);
    expect(recording.records).toEqual([
      { method: "POST", pathname: "/api/admin/v1/session", search: "", body: JSON.stringify(login) },
      { method: "DELETE", pathname: "/api/admin/v1/session", search: "", body: null },
      { method: "GET", pathname: "/api/admin/v1/me", search: "", body: null },
      { method: "GET", pathname: "/api/admin/v1/articles", search: "?state=trashed", body: null },
      { method: "POST", pathname: "/api/admin/v1/articles", search: "", body: null },
      { method: "GET", pathname: "/api/admin/v1/articles/11", search: "", body: null },
      { method: "PUT", pathname: "/api/admin/v1/articles/11/draft", search: "", body: JSON.stringify(saveDraft) },
      { method: "GET", pathname: "/api/admin/v1/articles/11/preview", search: "", body: null },
      { method: "GET", pathname: "/api/admin/v1/articles/11/versions", search: "", body: null },
      { method: "POST", pathname: "/api/admin/v1/articles/11/versions", search: "", body: JSON.stringify(lockVersion) },
      { method: "POST", pathname: "/api/admin/v1/articles/11/versions/41/restore", search: "", body: JSON.stringify(lockVersion) },
      { method: "POST", pathname: "/api/admin/v1/articles/11/trash", search: "", body: null },
      { method: "POST", pathname: "/api/admin/v1/articles/11/untrash", search: "", body: null },
      { method: "GET", pathname: "/api/admin/v1/tags", search: "", body: null },
      { method: "POST", pathname: "/api/admin/v1/tags", search: "", body: JSON.stringify({ name: "Go" }) },
      { method: "PATCH", pathname: "/api/admin/v1/tags/31", search: "", body: JSON.stringify({ name: "Go" }) },
      { method: "POST", pathname: "/api/admin/v1/media/upload-policy", search: "", body: null },
      { method: "POST", pathname: "/api/admin/v1/media", search: "", body: JSON.stringify(registerMedia) },
      { method: "GET", pathname: "/api/admin/v1/settings/site", search: "", body: null },
      { method: "PUT", pathname: "/api/admin/v1/settings/site", search: "", body: JSON.stringify(putSite) },
      { method: "GET", pathname: "/api/admin/v1/settings/hotlink", search: "", body: null },
      { method: "PUT", pathname: "/api/admin/v1/settings/hotlink", search: "", body: JSON.stringify(putHotlink) },
      { method: "GET", pathname: "/api/admin/v1/builder", search: "", body: null },
      { method: "PUT", pathname: "/api/admin/v1/builder", search: "", body: JSON.stringify(putBuilder) },
      { method: "POST", pathname: "/api/admin/v1/builder/test", search: "", body: null },
      { method: "GET", pathname: "/api/admin/v1/releases", search: "?limit=20&offset=0", body: null },
      { method: "POST", pathname: "/api/admin/v1/releases", search: "", body: JSON.stringify(createRelease) },
      { method: "GET", pathname: "/api/admin/v1/releases/71", search: "", body: null },
      { method: "POST", pathname: "/api/admin/v1/releases/71/retry", search: "", body: null },
    ]);
  });

  it("turns an unauthenticated 401 Problem into ApiProblem and invokes the callback only for that code", async () => {
    const onUnauthenticated = vi.fn();
    const recording = createRecordingFetch([
      {
        status: 401,
        contentType: "application/problem+json",
        body: {
          type: "https://qiuxs.com/problems/unauthenticated",
          title: "Authentication required",
          status: 401,
          code: "unauthenticated",
          requestId: "req-auth",
        },
      },
      {
        status: 401,
        contentType: "application/problem+json",
        body: {
          type: "https://qiuxs.com/problems/future_service_code",
          title: "Future authentication response",
          status: 401,
          code: "future_service_code",
          requestId: "req-future-auth",
        },
      },
    ]);
    const api = createAdminApi({ fetch: recording.fetch, onUnauthenticated });

    await expect(api.getCurrentAdmin()).rejects.toMatchObject({
      status: 401,
      code: "unauthenticated",
      requestId: "req-auth",
    });
    expect(onUnauthenticated).toHaveBeenCalledTimes(1);
    await expect(api.getCurrentAdmin()).rejects.toMatchObject({
      status: 401,
      code: "future_service_code",
      requestId: "req-future-auth",
    });
    expect(onUnauthenticated).toHaveBeenCalledTimes(1);
  });

  it.each([
    ["builder_conflict", 409],
    ["future_service_code", 503],
  ])("preserves non-exhaustive service code %s", async (code, status) => {
    const recording = createRecordingFetch([{
      status,
      contentType: "application/problem+json",
      body: {
        type: `https://qiuxs.com/problems/${code}`,
        title: "Service request failed",
        status,
        code,
        requestId: "req-service",
      },
    }]);
    const api = createAdminApi({ fetch: recording.fetch });

    await expect(api.getBuilderConfig()).rejects.toMatchObject({
      status,
      code,
      requestId: "req-service",
    });
  });

  it("rejects invalid path and returned entity IDs before they cross the boundary", async () => {
    const invalidReturnedId = Number.MAX_SAFE_INTEGER + 1;
    const recording = createRecordingFetch([
      json(200, { ...articleDetail, id: invalidReturnedId }),
    ]);
    const api = createAdminApi({ fetch: recording.fetch });

    expect(() => requireEntityId(0, "articleId")).toThrow(ApiProblem);
    expect(() => requireEntityId(1.5, "articleId")).toThrow(ApiProblem);
    expect(() => requireEntityId(invalidReturnedId, "articleId")).toThrow(ApiProblem);
    await expect(api.getArticle(0)).rejects.toMatchObject({
      status: 502,
      code: "invalid_api_response",
      requestId: "client",
    });
    await expect(api.getArticle(11)).rejects.toMatchObject({
      status: 502,
      code: "invalid_api_response",
      requestId: "client",
    });
  });

  it("accepts only the documented success status and response data", async () => {
    const recording = createRecordingFetch([
      json(200, articleDetail),
      empty(201),
      json(200, {}),
    ]);
    const api = createAdminApi({ fetch: recording.fetch });

    await expect(api.createArticle()).rejects.toMatchObject({ code: "invalid_api_response" });
    await expect(api.createArticle()).rejects.toMatchObject({ code: "invalid_api_response" });
    await expect(api.logoutAdmin()).rejects.toMatchObject({ code: "invalid_api_response" });
  });

  it("never exposes submitted passwords, tokens, network messages, or raw invalid response bodies", async () => {
    const password = "password-never-log";
    const token = "token-never-log";
    const networkFetch: typeof globalThis.fetch = async () => {
      throw new Error(`network failed with ${password}`);
    };
    const loginApi = createAdminApi({ fetch: networkFetch });
    const loginError = await loginApi.loginAdmin({ username: "admin", password }).catch((error: unknown) => error);

    expect(loginError).toBeInstanceOf(ApiProblem);
    expect(String(loginError)).not.toContain(password);
    expect(JSON.stringify(loginError)).not.toContain(password);

    const invalidResponseFetch: typeof globalThis.fetch = async () => new Response(
      `upstream echoed ${token}`,
      { status: 502, headers: { "Content-Type": "application/json" } },
    );
    const builderApi = createAdminApi({ fetch: invalidResponseFetch });
    const builderError = await builderApi.putBuilderConfig({
      name: "home-jenkins",
      baseUrl: "https://jenkins.example.com",
      username: "blog-builder",
      token,
      jobName: "qiuxs-blog-site",
      enabled: true,
    }).catch((error: unknown) => error);

    expect(builderError).toBeInstanceOf(ApiProblem);
    expect(builderError).toMatchObject({ code: "invalid_api_response", requestId: "client" });
    expect(String(builderError)).not.toContain(token);
    expect(JSON.stringify(builderError)).not.toContain(token);
    expect(String(builderError)).not.toContain("upstream echoed");
  });
});

describe("queryKeys", () => {
  it("keeps all article data below articlesRoot", () => {
    expect(queryKeys.articlesRoot).toEqual(["articles"]);
    expect(queryKeys.articleList("active")).toEqual(["articles", "list", "active"]);
    expect(queryKeys.article(11)).toEqual(["articles", "detail", 11]);
    expect(queryKeys.articlePreview(11)).toEqual(["articles", "detail", 11, "preview"]);
    expect(queryKeys.articleVersions(11)).toEqual(["articles", "detail", 11, "versions"]);
  });

  it("separates release list invalidation from seeded release details", () => {
    expect(queryKeys.releasesRoot).toEqual(["releases"]);
    expect(queryKeys.releaseListsRoot).toEqual(["releases", "list"]);
    expect(queryKeys.releaseList(20, 0)).toEqual(["releases", "list", 20, 0]);
    expect(queryKeys.release(71)).toEqual(["releases", "detail", 71]);
  });

  it("defines stable singleton keys", () => {
    expect(queryKeys.me).toEqual(["me"]);
    expect(queryKeys.tags).toEqual(["tags"]);
    expect(queryKeys.site).toEqual(["settings", "site"]);
    expect(queryKeys.hotlink).toEqual(["settings", "hotlink"]);
    expect(queryKeys.builder).toEqual(["builder"]);
  });
});
