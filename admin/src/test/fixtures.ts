import type {
  ArticleDetail,
  ArticleList,
  ArticleSummary,
  BuilderConfigView,
  DraftView,
  HotlinkSettingsView,
  MediaUploadPolicy,
  MediaView,
  PreviewView,
  Problem,
  PublishJobView,
  ReleaseList,
  ReleaseView,
  RevisionList,
  RevisionView,
  SiteSettingsView,
  TagList,
  TagView,
  VersionResult,
} from "../api/admin-api";

export const draftView = {
  id: 21, articleId: 11, revisionNo: 1, lockVersion: 7,
  status: "editing", reason: "draft", title: "Build log", summary: "Summary",
  coverMediaId: null, contentMd: "# Build log\n", contentHash: "sha256:draft",
  tags: [{ tagId: 31, name: "Go", slug: "go", position: 0 }], media: [],
  createdAt: "2026-08-13T00:00:00Z", updatedAt: "2026-08-14T00:00:00Z",
} satisfies DraftView;

export const articleDetail = {
  id: 11, slug: "abc123_def45", draftRevisionId: 21, publishedRevisionId: null,
  state: "active", createdAt: "2026-08-13T00:00:00Z",
  updatedAt: "2026-08-14T00:00:00Z", draft: draftView,
} satisfies ArticleDetail;

export const mediaPolicy = {
  uploadUrl: "https://gfs.test/v1/upload", appId: "blog", policy: "cG9saWN5",
  signature: "0123456789abcdef", timestamp: "1786636800", expire: "60",
  nonce: "abcdefghijklmnopqrstuv", fileField: "file",
} satisfies MediaUploadPolicy;

export const mediaView = {
  id: 51, publicKey: "m_abcdefghijklmnopqrstuv", gfsFileId: 41,
  originalName: "photo.png", mimeType: "image/png", fileSize: 8192,
  width: 640, height: 480, state: "active",
  url: "/img/proxy/m_abcdefghijklmnopqrstuv",
  createdAt: "2026-08-14T00:00:00Z", updatedAt: "2026-08-14T00:00:00Z",
} satisfies MediaView;

export const siteSettings = {
  id: null, lockVersion: 0, siteName: "qiuxs", authorName: "qiuxs", authorBio: "",
  homeStatus: "", aboutMd: "", socialLinks: [], seoDefaultTitle: "",
  seoDefaultDescription: "", seoDefaultImageMediaId: null,
  filingName: "长安休息室", filingNumber: "浙ICP备17057726号-1",
  filingUrl: "https://beian.miit.gov.cn/", updatedAt: null,
} satisfies SiteSettingsView;

export const hotlinkSettings = {
  allowEmptyReferer: true,
  entries: [{ hostname: "qiuxs.com", enabled: true }, { hostname: "blog-admin.qiuxs.com", enabled: true }],
} satisfies HotlinkSettingsView;

export const builderConfig = {
  id: 61, name: "home-jenkins", baseUrl: "https://jenkins.example.com",
  username: "blog-builder", jobName: "qiuxs-blog-site", enabled: true,
  tokenConfigured: true,
} satisfies BuilderConfigView;

export const failedJob = {
  id: 81, releaseId: 71, builderId: 61,
  builderTarget: { name: "home-jenkins", baseUrl: "https://jenkins.example.com", username: "blog-builder", jobName: "qiuxs-blog-site" },
  status: "failed", stage: "build", buildNumber: 123, errorSummary: "Build failed",
  createdAt: "2026-08-14T00:00:00Z", finishedAt: "2026-08-14T00:01:00Z",
} satisfies PublishJobView;

export const failedRelease = {
  id: 71, status: "failed", checksum: `sha256:${"a".repeat(64)}`,
  createdAt: "2026-08-14T00:00:00Z", completedAt: "2026-08-14T00:01:00Z",
  latestJob: failedJob, jobs: [failedJob],
} satisfies ReleaseView;

export const articleSummary = {
  id: 11, slug: "abc123_def45", draftRevisionId: 21, publishedRevisionId: null,
  state: "active", draftTitle: "Build log", draftUpdatedAt: "2026-08-14T00:00:00Z",
  createdAt: "2026-08-13T00:00:00Z", updatedAt: "2026-08-14T00:00:00Z",
} satisfies ArticleSummary;
export const articleList = { items: [articleSummary] } satisfies ArticleList;
export const previewView = { slug: articleDetail.slug, draft: draftView } satisfies PreviewView;

export const revisionView = {
  ...draftView, id: 41, status: "frozen", reason: "manual_version",
} satisfies RevisionView;
export const revisionList = { items: [revisionView] } satisfies RevisionList;
export const versionResult = { version: revisionView, draft: draftView } satisfies VersionResult;

export const tagView = {
  id: 31, name: "Go", slug: "go",
  createdAt: "2026-08-13T00:00:00Z", updatedAt: "2026-08-13T00:00:00Z",
} satisfies TagView;
export const tagList = { items: [tagView] } satisfies TagList;
export const releaseList = { items: [failedRelease] } satisfies ReleaseList;
export const dependencyProblem = {
  type: "https://qiuxs.com/problems/dependency_unavailable",
  title: "Dependency unavailable", status: 503,
  code: "dependency_unavailable", requestId: "req-fixture",
} satisfies Problem;
