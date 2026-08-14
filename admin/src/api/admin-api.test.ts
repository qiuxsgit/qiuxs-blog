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
  dependencyProblem,
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

interface InvalidResponseShapeCase {
  name: string;
  response: PlannedResponse;
  invoke: (api: AdminApi) => Promise<unknown>;
}

function createRecordingFetch(planned: PlannedResponse[]) {
  const records: RequestRecord[] = [];
  const signals: AbortSignal[] = [];
  const fetch: typeof globalThis.fetch = async (input, init) => {
    const request = new Request(input, init);
    signals.push(request.signal);
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
  return { fetch, records, signals };
}

const json = (status: number, body: unknown): PlannedResponse => ({ status, body });
const empty = (status: number): PlannedResponse => ({ status });

function invalidFields(
  family: string,
  status: number,
  value: Record<string, unknown>,
  fields: ReadonlyArray<readonly [string, unknown]>,
  invoke: (api: AdminApi) => Promise<unknown>,
  wrap: (invalid: Record<string, unknown>) => unknown = (invalid) => invalid,
): InvalidResponseShapeCase[] {
  return fields.map(([field, invalid]) => ({
    name: `${family}.${field}`,
    response: json(status, wrap({ ...value, [field]: invalid })),
    invoke,
  }));
}

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
    const controller = new AbortController();
    const signal = controller.signal;

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

    expect(await api.loginAdmin(login, signal)).toEqual({ id: 1, username: "admin" });
    expect(await api.logoutAdmin(signal)).toBeUndefined();
    expect(await api.getCurrentAdmin(signal)).toEqual({ id: 1, username: "admin" });
    expect(await api.listArticles({ state: "trashed" }, signal)).toEqual(articleList);
    expect(await api.createArticle(signal)).toEqual(articleDetail);
    expect(await api.getArticle(11, signal)).toEqual(articleDetail);
    expect(await api.saveArticleDraft(11, saveDraft, signal)).toEqual(draftView);
    expect(await api.getArticlePreview(11, signal)).toEqual(previewView);
    expect(await api.listArticleVersions(11, signal)).toEqual(revisionList);
    expect(await api.createArticleVersion(11, lockVersion, signal)).toEqual(versionResult);
    expect(await api.restoreArticleVersion(11, 41, lockVersion, signal)).toEqual(draftView);
    expect(await api.trashArticle(11, signal)).toBeUndefined();
    expect(await api.untrashArticle(11, signal)).toBeUndefined();
    expect(await api.listTags(signal)).toEqual(tagList);
    expect(await api.createTag({ name: "Go" }, signal)).toEqual(tagView);
    expect(await api.renameTag(31, { name: "Go" }, signal)).toEqual(tagView);
    expect(await api.createMediaUploadPolicy(signal)).toEqual(mediaPolicy);
    expect(await api.registerMedia(registerMedia, signal)).toEqual(mediaView);
    expect(await api.getSiteSettings(signal)).toEqual(siteSettings);
    expect(await api.putSiteSettings(putSite, signal)).toEqual(siteSettings);
    expect(await api.getHotlinkSettings(signal)).toEqual(hotlinkSettings);
    expect(await api.putHotlinkSettings(putHotlink, signal)).toEqual(hotlinkSettings);
    expect(await api.getBuilderConfig(signal)).toEqual(builderConfig);
    expect(await api.putBuilderConfig(putBuilder, signal)).toEqual(builderConfig);
    expect(await api.testBuilderConfig(signal)).toBeUndefined();
    expect(await api.listReleases({ limit: 20, offset: 0 }, signal)).toEqual(releaseList);
    expect(await api.createRelease(createRelease, signal)).toEqual({ release: failedRelease, job: failedJob });
    expect(await api.getRelease(71, signal)).toEqual(failedRelease);
    expect(await api.retryRelease(71, signal)).toEqual({ release: failedRelease, job: failedJob });

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
    controller.abort();
    expect(recording.signals).toHaveLength(29);
    expect(recording.signals.every((requestSignal) => requestSignal.aborted)).toBe(true);
  });

  it("preserves an AbortError from the fetch boundary", async () => {
    const controller = new AbortController();
    const reason = new DOMException("cancelled", "AbortError");
    controller.abort(reason);
    const abortingFetch: typeof globalThis.fetch = async (input) => {
      const request = new Request(input);
      if (!request.signal.aborted) {
        throw new Error("AbortSignal was not forwarded");
      }
      throw request.signal.reason;
    };
    const api = createAdminApi({ fetch: abortingFetch });

    const error = await api.getCurrentAdmin(controller.signal).catch((cause: unknown) => cause);

    expect(error).toBe(reason);
    expect(error).toMatchObject({ name: "AbortError" });
    expect(error).not.toBeInstanceOf(ApiProblem);
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

  it("serializes a safe Problem title and redacts only fields containing the submitted password", async () => {
    const password = "password-problem-secret";
    const recording = createRecordingFetch([{
      status: 401,
      contentType: "application/problem+json",
      body: {
        type: `https://qiuxs.com/problems/${password}`,
        title: `Authentication rejected for ${password}`,
        status: 401,
        code: "future_service_code",
        requestId: `req-${password}`,
      },
    }]);
    const api = createAdminApi({ fetch: recording.fetch });

    const error = await api.loginAdmin({ username: "admin", password }).catch((cause: unknown) => cause);

    expect(error).toBeInstanceOf(ApiProblem);
    const problem = error as ApiProblem;
    expect(problem).toMatchObject({
      title: "Request failed",
      code: "future_service_code",
      requestId: "redacted",
      type: "about:blank",
    });
    expect(JSON.stringify(problem)).not.toContain(password);
  });

  it("preserves safe Problem fields while redacting a code containing the submitted builder token", async () => {
    const token = "builder-problem-secret";
    const recording = createRecordingFetch([{
      status: 409,
      contentType: "application/problem+json",
      body: {
        type: "https://qiuxs.com/problems/builder_conflict",
        title: "Builder conflict",
        status: 409,
        code: `builder_conflict_${token}`,
        requestId: "req-builder-safe",
      },
    }]);
    const api = createAdminApi({ fetch: recording.fetch });

    const error = await api.putBuilderConfig({
      name: "home-jenkins",
      baseUrl: "https://jenkins.example.com",
      username: "blog-builder",
      token,
      jobName: "qiuxs-blog-site",
      enabled: true,
    }).catch((cause: unknown) => cause);

    expect(error).toBeInstanceOf(ApiProblem);
    const problem = error as ApiProblem;
    expect(problem).toMatchObject({
      title: "Builder conflict",
      code: "redacted",
      requestId: "req-builder-safe",
      type: "https://qiuxs.com/problems/builder_conflict",
    });
    expect(JSON.stringify(problem)).not.toContain(token);
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

  const validSaveDraft: SaveDraftRequest = {
    lockVersion: 1,
    title: "Build log",
    summary: "Summary",
    coverMediaId: null,
    contentMd: "# Build log\n",
    tagIds: [1],
  };
  const validPutSite: PutSiteSettingsRequest = {
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

  it.each<[string, (api: AdminApi) => Promise<unknown>]>([
    ["save draft lockVersion unsafe", (api) => api.saveArticleDraft(11, { ...validSaveDraft, lockVersion: Number.MAX_SAFE_INTEGER + 1 })],
    ["save draft coverMediaId below minimum", (api) => api.saveArticleDraft(11, { ...validSaveDraft, coverMediaId: 0 })],
    ["save draft tagIds fractional", (api) => api.saveArticleDraft(11, { ...validSaveDraft, tagIds: [1.5] })],
    ["create version lockVersion below minimum", (api) => api.createArticleVersion(11, { lockVersion: 0 })],
    ["restore version lockVersion unsafe", (api) => api.restoreArticleVersion(11, 41, { lockVersion: Number.MAX_SAFE_INTEGER + 1 })],
    ["register media gfsFileId fractional", (api) => api.registerMedia({ gfsFileId: 1.5, originalName: "photo.png" })],
    ["site lockVersion below minimum", (api) => api.putSiteSettings({ ...validPutSite, lockVersion: -1 })],
    ["site seoDefaultImageMediaId unsafe", (api) => api.putSiteSettings({ ...validPutSite, seoDefaultImageMediaId: Number.MAX_SAFE_INTEGER + 1 })],
    ["release articleId below minimum", (api) => api.createRelease({ mode: "publish_article", articleId: 0 })],
  ])("rejects request int64 before fetch: %s", async (_name, invoke) => {
    let fetchCalls = 0;
    const fetch: typeof globalThis.fetch = async () => {
      fetchCalls += 1;
      return new Response("unexpected fetch", { status: 500, headers: { "Content-Type": "text/plain" } });
    };
    const api = createAdminApi({ fetch });

    await expect(invoke(api)).rejects.toMatchObject({
      status: 502,
      code: "invalid_api_response",
      requestId: "client",
    });
    expect(fetchCalls).toBe(0);
  });

  it.each<[string, PlannedResponse, (api: AdminApi) => Promise<unknown>]>([
    ["draft revisionNo unsafe", json(200, {
      ...articleDetail,
      draft: { ...draftView, revisionNo: Number.MAX_SAFE_INTEGER + 1 },
    }), (api) => api.getArticle(11)],
    ["draft lockVersion below minimum", json(200, {
      ...previewView,
      draft: { ...draftView, lockVersion: 0 },
    }), (api) => api.getArticlePreview(11)],
    ["media fileSize fractional", json(201, { ...mediaView, fileSize: 1.5 }), (api) => api.registerMedia({ gfsFileId: 41, originalName: "photo.png" })],
    ["site lockVersion below minimum", json(200, { ...siteSettings, lockVersion: -1 }), (api) => api.getSiteSettings()],
    ["publish job buildNumber unsafe", json(200, {
      ...failedRelease,
      latestJob: { ...failedJob, buildNumber: Number.MAX_SAFE_INTEGER + 1 },
    }), (api) => api.getRelease(71)],
  ])("rejects response int64: %s", async (_name, response, invoke) => {
    const recording = createRecordingFetch([response]);
    const api = createAdminApi({ fetch: recording.fetch });

    await expect(invoke(api)).rejects.toMatchObject({
      status: 502,
      code: "invalid_api_response",
      requestId: "client",
    });
  });

  it("accepts int64 minimums and documented nullable values", async () => {
    const releaseWithNullBuild = {
      ...failedRelease,
      latestJob: { ...failedJob, buildNumber: null },
      jobs: [{ ...failedJob, buildNumber: null }],
    };
    const recording = createRecordingFetch([
      json(200, { ...draftView, revisionNo: 1, lockVersion: 1, coverMediaId: null }),
      json(200, { ...siteSettings, lockVersion: 0, seoDefaultImageMediaId: null }),
      json(201, { ...mediaView, fileSize: 1 }),
      json(202, { release: failedRelease, job: failedJob }),
      json(200, releaseWithNullBuild),
    ]);
    const api = createAdminApi({ fetch: recording.fetch });

    await expect(api.saveArticleDraft(11, validSaveDraft)).resolves.toMatchObject({ lockVersion: 1 });
    await expect(api.putSiteSettings(validPutSite)).resolves.toMatchObject({ lockVersion: 0 });
    await expect(api.registerMedia({ gfsFileId: 1, originalName: "photo.png" })).resolves.toMatchObject({ fileSize: 1 });
    await expect(api.createRelease({ mode: "publish_settings", articleId: null })).resolves.toMatchObject({ release: failedRelease });
    await expect(api.getRelease(71)).resolves.toMatchObject({ latestJob: { buildNumber: null } });
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

  it.each<[string, () => Response, string]>([
    ["malformed JSON", () => new Response("malformed-json-secret", {
      status: 200,
      headers: { "Content-Type": "application/json" },
    }), "malformed-json-secret"],
    ["wrong success content type", () => new Response(JSON.stringify({ id: 1, username: "admin" }), {
      status: 200,
      headers: { "Content-Type": "application/problem+json" },
    }), "admin"],
    ["null success data", () => new Response("null", {
      status: 200,
      headers: { "Content-Type": "application/json" },
    }), "null"],
  ])("maps %s to a fixed invalid-response Problem", async (_name, response, rawValue) => {
    const fetch: typeof globalThis.fetch = async () => response();
    const api = createAdminApi({ fetch });

    const error = await api.getCurrentAdmin().catch((cause: unknown) => cause);

    expect(error).toBeInstanceOf(ApiProblem);
    expect(error).toMatchObject({
      status: 502,
      code: "invalid_api_response",
      requestId: "client",
      title: "Invalid API response",
    });
    expect(String(error)).not.toContain(rawValue);
    expect(JSON.stringify(error)).not.toContain(rawValue);
  });

  it.each<[string, PlannedResponse, (api: AdminApi) => Promise<unknown>]>([
    ["admin missing username", json(200, { id: 1 }), (api) => api.getCurrentAdmin()],
    ["Problem-shaped media policy", json(200, dependencyProblem), (api) => api.createMediaUploadPolicy()],
    ["Problem-shaped hotlink settings", json(200, dependencyProblem), (api) => api.getHotlinkSettings()],
  ])("rejects application/json success missing its required shape: %s", async (_name, response, invoke) => {
    const recording = createRecordingFetch([response]);
    const api = createAdminApi({ fetch: recording.fetch });

    await expect(invoke(api)).rejects.toMatchObject({
      status: 502,
      code: "invalid_api_response",
      requestId: "client",
      title: "Invalid API response",
    });
  });

  const mediaReference = {
    mediaId: 51,
    publicKey: "m_abcdefghijklmnopqrstuv",
    purpose: "content",
    position: 0,
  };
  const draftWithMedia = { ...draftView, media: [mediaReference] };
  const responseShapeCases: InvalidResponseShapeCase[] = [
    ...invalidFields("Admin", 200, { id: 1, username: "admin" }, [
      ["id", 0],
      ["username", 42],
    ], (api) => api.getCurrentAdmin()),
    ...invalidFields("ArticleSummary", 200, articleSummary, [
      ["id", 0],
      ["slug", null],
      ["draftRevisionId", 0],
      ["publishedRevisionId", false],
      ["state", "archived"],
      ["draftTitle", 42],
      ["draftUpdatedAt", false],
      ["createdAt", 42],
      ["updatedAt", undefined],
    ], (api) => api.listArticles(), (invalid) => ({ items: [invalid] })),
    ...invalidFields("ArticleList", 200, articleList, [
      ["items", null],
    ], (api) => api.listArticles()),
    ...invalidFields("ArticleDetail", 200, articleDetail, [
      ["id", 0],
      ["slug", 42],
      ["draftRevisionId", 0],
      ["publishedRevisionId", false],
      ["state", "archived"],
      ["createdAt", null],
      ["updatedAt", undefined],
      ["draft", []],
    ], (api) => api.getArticle(11)),
    ...invalidFields("DraftView", 200, draftWithMedia, [
      ["id", 0],
      ["articleId", 0],
      ["revisionNo", 0],
      ["lockVersion", 0],
      ["status", "frozen"],
      ["reason", "manual_version"],
      ["title", 42],
      ["summary", null],
      ["coverMediaId", false],
      ["contentMd", false],
      ["contentHash", undefined],
      ["tags", {}],
      ["media", null],
      ["createdAt", 42],
      ["updatedAt", false],
    ], (api) => api.saveArticleDraft(11, validSaveDraft)),
    ...invalidFields("TagSnapshot", 200, draftView.tags[0] as Record<string, unknown>, [
      ["tagId", 0],
      ["name", 42],
      ["slug", null],
      ["position", 1.5],
    ], (api) => api.saveArticleDraft(11, validSaveDraft), (invalid) => ({
      ...draftView,
      tags: [invalid],
    })),
    ...invalidFields("MediaReference", 200, mediaReference, [
      ["mediaId", 0],
      ["publicKey", 42],
      ["purpose", "thumbnail"],
      ["position", -1],
    ], (api) => api.saveArticleDraft(11, validSaveDraft), (invalid) => ({
      ...draftView,
      media: [invalid],
    })),
    ...invalidFields("PreviewView", 200, previewView, [
      ["slug", null],
      ["draft", "draft"],
    ], (api) => api.getArticlePreview(11)),
    ...invalidFields("RevisionView", 200, revisionList.items[0] as Record<string, unknown>, [
      ["id", 0],
      ["articleId", 0],
      ["revisionNo", 0],
      ["lockVersion", 0],
      ["status", "editing"],
      ["reason", "draft"],
      ["title", 42],
      ["summary", null],
      ["coverMediaId", false],
      ["contentMd", false],
      ["contentHash", undefined],
      ["tags", {}],
      ["media", null],
      ["createdAt", 42],
      ["updatedAt", false],
    ], (api) => api.listArticleVersions(11), (invalid) => ({ items: [invalid] })),
    ...invalidFields("RevisionList", 200, revisionList, [
      ["items", {}],
    ], (api) => api.listArticleVersions(11)),
    ...invalidFields("VersionResult", 201, versionResult, [
      ["version", null],
      ["draft", []],
    ], (api) => api.createArticleVersion(11, { lockVersion: 7 })),
    ...invalidFields("TagView", 201, tagView, [
      ["id", 0],
      ["name", 42],
      ["slug", null],
      ["createdAt", false],
      ["updatedAt", undefined],
    ], (api) => api.createTag({ name: "Go" })),
    ...invalidFields("TagList", 200, tagList, [
      ["items", null],
    ], (api) => api.listTags()),
    ...invalidFields("MediaUploadPolicy", 200, mediaPolicy, [
      ["uploadUrl", 42],
      ["appId", null],
      ["policy", false],
      ["signature", undefined],
      ["timestamp", 42],
      ["expire", null],
      ["nonce", false],
      ["fileField", 42],
    ], (api) => api.createMediaUploadPolicy()),
    ...invalidFields("MediaView", 201, mediaView, [
      ["id", 0],
      ["publicKey", 42],
      ["gfsFileId", 0],
      ["originalName", null],
      ["mimeType", "image/svg+xml"],
      ["fileSize", 0],
      ["width", 1.5],
      ["height", 0],
      ["state", "deleted"],
      ["url", false],
      ["createdAt", 42],
      ["updatedAt", undefined],
    ], (api) => api.registerMedia({ gfsFileId: 41, originalName: "photo.png" })),
    ...invalidFields("SiteSettingsView", 200, {
      ...siteSettings,
      socialLinks: [{ label: "GitHub", url: "https://github.com/qiuxs" }],
    }, [
      ["id", false],
      ["lockVersion", -1],
      ["siteName", 42],
      ["authorName", null],
      ["authorBio", false],
      ["homeStatus", undefined],
      ["aboutMd", 42],
      ["socialLinks", null],
      ["seoDefaultTitle", false],
      ["seoDefaultDescription", 42],
      ["seoDefaultImageMediaId", false],
      ["filingName", null],
      ["filingNumber", false],
      ["filingUrl", 42],
      ["updatedAt", {}],
    ], (api) => api.getSiteSettings()),
    ...invalidFields("SocialLink", 200, { label: "GitHub", url: "https://github.com/qiuxs" }, [
      ["label", 42],
      ["url", null],
    ], (api) => api.getSiteSettings(), (invalid) => ({
      ...siteSettings,
      socialLinks: [invalid],
    })),
    ...invalidFields("HotlinkSettingsView", 200, hotlinkSettings, [
      ["allowEmptyReferer", "yes"],
      ["entries", null],
    ], (api) => api.getHotlinkSettings()),
    ...invalidFields("HotlinkEntry", 200, hotlinkSettings.entries[0] as Record<string, unknown>, [
      ["hostname", 42],
      ["enabled", "yes"],
    ], (api) => api.getHotlinkSettings(), (invalid) => ({
      ...hotlinkSettings,
      entries: [invalid],
    })),
    ...invalidFields("BuilderConfigView", 200, builderConfig, [
      ["id", 0],
      ["name", 42],
      ["baseUrl", null],
      ["username", false],
      ["jobName", undefined],
      ["enabled", "yes"],
      ["tokenConfigured", 1],
    ], (api) => api.getBuilderConfig()),
    ...invalidFields("ReleaseView", 200, failedRelease, [
      ["id", 0],
      ["status", "cancelled"],
      ["checksum", 42],
      ["createdAt", null],
      ["completedAt", {}],
      ["latestJob", []],
      ["jobs", null],
      ["jobs", []],
    ], (api) => api.getRelease(71)),
    ...invalidFields("PublishJobView", 200, failedJob, [
      ["id", 0],
      ["releaseId", 0],
      ["builderId", 0],
      ["builderTarget", null],
      ["status", "cancelled"],
      ["stage", 42],
      ["buildNumber", false],
      ["errorSummary", null],
      ["createdAt", false],
      ["finishedAt", {}],
    ], (api) => api.getRelease(71), (invalid) => ({
      ...failedRelease,
      latestJob: invalid,
      jobs: [invalid],
    })),
    ...invalidFields("BuilderTargetView", 200, failedJob.builderTarget, [
      ["name", 42],
      ["baseUrl", null],
      ["username", false],
      ["jobName", undefined],
    ], (api) => api.getRelease(71), (invalid) => {
      const job = { ...failedJob, builderTarget: invalid };
      return { ...failedRelease, latestJob: job, jobs: [job] };
    }),
    ...invalidFields("ReleaseList", 200, releaseList, [
      ["items", {}],
    ], (api) => api.listReleases()),
    ...invalidFields("CreateReleaseResult", 202, { release: failedRelease, job: failedJob }, [
      ["release", null],
      ["job", []],
    ], (api) => api.createRelease({ mode: "publish_settings", articleId: null })),
    ...invalidFields("RetryReleaseResult", 200, { release: failedRelease, job: failedJob }, [
      ["release", []],
      ["job", null],
    ], (api) => api.retryRelease(71)),
  ];

  it.each(responseShapeCases)("rejects invalid documented response shape: $name", async ({ response, invoke }) => {
    const recording = createRecordingFetch([response]);
    const api = createAdminApi({ fetch: recording.fetch });

    const error = await invoke(api).catch((cause: unknown) => cause);

    expect(error).toBeInstanceOf(ApiProblem);
    expect(error).toMatchObject({
      status: 502,
      code: "invalid_api_response",
      requestId: "client",
      title: "Invalid API response",
    });
    expect(JSON.stringify(error)).not.toContain("archived");
    expect(JSON.stringify(error)).not.toContain("cancelled");
  });

  it("accepts null only for documented nullable response fields", async () => {
    const nullableRelease = {
      ...failedRelease,
      completedAt: null,
      latestJob: { ...failedJob, buildNumber: null, finishedAt: null },
      jobs: [{ ...failedJob, buildNumber: null, finishedAt: null }],
    };
    const recording = createRecordingFetch([
      json(200, { items: [{ ...articleSummary, publishedRevisionId: null }] }),
      json(200, { ...draftView, coverMediaId: null }),
      json(200, { ...siteSettings, id: null, seoDefaultImageMediaId: null, updatedAt: null }),
      json(200, nullableRelease),
    ]);
    const api = createAdminApi({ fetch: recording.fetch });

    await expect(api.listArticles()).resolves.toEqual({ items: [{ ...articleSummary, publishedRevisionId: null }] });
    await expect(api.saveArticleDraft(11, validSaveDraft)).resolves.toMatchObject({ coverMediaId: null });
    await expect(api.getSiteSettings()).resolves.toMatchObject({ id: null, seoDefaultImageMediaId: null, updatedAt: null });
    await expect(api.getRelease(71)).resolves.toMatchObject({
      completedAt: null,
      latestJob: { buildNumber: null, finishedAt: null },
    });
  });

  it("maps only a real fetch rejection to the fixed network Problem", async () => {
    const rawMessage = "socket failure must stay private";
    const fetch: typeof globalThis.fetch = async () => {
      throw new Error(rawMessage);
    };
    const api = createAdminApi({ fetch });

    const error = await api.getCurrentAdmin().catch((cause: unknown) => cause);

    expect(error).toBeInstanceOf(ApiProblem);
    expect(error).toMatchObject({
      status: 503,
      code: "network_error",
      requestId: "client",
      title: "Network request failed",
    });
    expect(String(error)).not.toContain(rawMessage);
    expect(JSON.stringify(error)).not.toContain(rawMessage);
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
