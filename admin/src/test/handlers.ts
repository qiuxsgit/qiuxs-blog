import { http, HttpResponse } from "msw";

import {
  articleDetail,
  articleList,
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
} from "./fixtures";

const adminView = { id: 1, username: "admin" };

export const handlers = [
  http.post("*/api/admin/v1/session", () => HttpResponse.json(adminView)),
  http.delete("*/api/admin/v1/session", () => new HttpResponse(null, { status: 204 })),
  http.get("*/api/admin/v1/me", () => HttpResponse.json(adminView)),

  http.get("*/api/admin/v1/articles", () => HttpResponse.json(articleList)),
  http.post("*/api/admin/v1/articles", () => HttpResponse.json(articleDetail, { status: 201 })),
  http.get("*/api/admin/v1/articles/:articleId", () => HttpResponse.json(articleDetail)),
  http.put("*/api/admin/v1/articles/:articleId/draft", () => HttpResponse.json(draftView)),
  http.get("*/api/admin/v1/articles/:articleId/preview", () => HttpResponse.json(previewView)),
  http.get("*/api/admin/v1/articles/:articleId/versions", () => HttpResponse.json(revisionList)),
  http.post("*/api/admin/v1/articles/:articleId/versions", () => HttpResponse.json(versionResult, { status: 201 })),
  http.post("*/api/admin/v1/articles/:articleId/versions/:revisionId/restore", () => HttpResponse.json(draftView)),
  http.post("*/api/admin/v1/articles/:articleId/trash", () => new HttpResponse(null, { status: 204 })),
  http.post("*/api/admin/v1/articles/:articleId/untrash", () => new HttpResponse(null, { status: 204 })),

  http.get("*/api/admin/v1/tags", () => HttpResponse.json(tagList)),
  http.post("*/api/admin/v1/tags", () => HttpResponse.json(tagView, { status: 201 })),
  http.patch("*/api/admin/v1/tags/:tagId", () => HttpResponse.json(tagView)),

  http.post("*/api/admin/v1/media/upload-policy", () => HttpResponse.json(mediaPolicy)),
  http.post("*/api/admin/v1/media", () => HttpResponse.json(mediaView, { status: 201 })),

  http.get("*/api/admin/v1/settings/site", () => HttpResponse.json(siteSettings)),
  http.put("*/api/admin/v1/settings/site", () => HttpResponse.json(siteSettings)),
  http.get("*/api/admin/v1/settings/hotlink", () => HttpResponse.json(hotlinkSettings)),
  http.put("*/api/admin/v1/settings/hotlink", () => HttpResponse.json(hotlinkSettings)),

  http.get("*/api/admin/v1/builder", () => HttpResponse.json(builderConfig)),
  http.put("*/api/admin/v1/builder", () => HttpResponse.json(builderConfig)),
  http.post("*/api/admin/v1/builder/test", () => new HttpResponse(null, { status: 204 })),

  http.get("*/api/admin/v1/releases", () => HttpResponse.json(releaseList)),
  http.post("*/api/admin/v1/releases", () => HttpResponse.json(
    { release: failedRelease, job: failedJob },
    { status: 202 },
  )),
  http.get("*/api/admin/v1/releases/:releaseId", () => HttpResponse.json(failedRelease)),
  http.post("*/api/admin/v1/releases/:releaseId/retry", () => HttpResponse.json(
    { release: failedRelease, job: failedJob },
    { status: 202 },
  )),
];
