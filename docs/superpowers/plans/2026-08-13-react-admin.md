# React Admin Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **Frontend verification policy:** Do not write React/DOM/UI unit tests and do not run Playwright or another automated UI acceptance gate. Frontend tests are limited to pure functions, deterministic state machines, serialization/validation, cache helpers, and API contract adapters. The owner will manually accept the completed Admin UI. Historical UI-test entries below are not release gates and must be omitted or removed during implementation.

**Goal:** Build the protected React Admin SPA for article editing, safe preview, direct GFS image upload, immutable versions and Releases, and site, builder, and hotlink settings.

**Architecture:** `admin/` is a same-origin Vite SPA. OpenAPI generates every wire type; an `openapi-fetch` adapter exposes operationId-named methods, TanStack Query owns server state, and editor-local state owns unsaved Markdown plus the two-second optimistic-lock autosave state machine. Milkdown is the default canvas, source and preview are explicit modes/routes. UI behavior is manually accepted; deterministic pure logic and API contract adapters are unit-tested.

**Tech Stack:** Node.js 20.19.4, npm lockfile, React, TypeScript strict mode, Vite, React Router, TanStack Query, `openapi-typescript`, `openapi-fetch`, Milkdown Kit, unified/remark/rehype with Shiki, Vitest, and API contract fixtures.

## Global Constraints

- Run every Admin install, generation, test, and build command on a developer or Jenkins host with `node --version` exactly `v20.19.4`; do not add an Admin Docker build.
- Commit `admin/package-lock.json`; clean verification uses `npm ci`.
- Stage 4 changes Admin source, Admin tests, shared Markdown fixtures/styles, and Admin documentation only. It does not change service code, SQL, OpenAPI, Release orchestration, Jenkinsfiles, Nginx, or deployment scripts.
- `contracts/openapi/admin-v1.yaml` is the only API source of truth. Generate `admin/src/api/generated/admin.ts`; never hand-edit it and never maintain duplicate handwritten request/response interfaces.
- Before feature work, regenerate types. `createAdminApi(options) satisfies AdminApi` and fixtures using `satisfies` must make any contract drift a TypeScript failure.
- All API calls are same-origin, use `credentials: "include"`, and rely on the browser-generated `Origin` header for unsafe methods. Never add `Authorization` or persist a Session token.
- Treat every OpenAPI `int64` value as a JavaScript number only after `Number.isSafeInteger(value)` plus its schema minimum; entity IDs are positive, nullable IDs are checked only when non-null, and site lock version may be zero.
- Milkdown is the default editor. The `contentMd` string remains the one content truth for visual mode, source mode, autosave, versioning, image insertion, and preview.
- Support headings, paragraphs, emphasis, strikethrough, ordered/unordered/task lists, quotes, links, images, tables, inline code, and fenced code. Raw HTML, Mermaid, formulas, footnotes, MDX, and custom directives are rejected or inert.
- Autosave waits 2,000 ms after the latest edit, sends the current `lockVersion`, serializes writes, and never overwrites a `409 revision_conflict`. Failed or conflicting local content remains recoverable.
- Enforce service limits: title 200 Unicode code points, summary 600 Unicode code points, at most 32 ordered unique tags, tag names at most 64 Unicode code points, and raw Markdown at most 2 MiB. Also measure the encoded draft/site JSON envelope because those entire HTTP bodies are capped at 2 MiB.
- Image preflight accepts JPEG (`.jpg`/`.jpeg`), PNG, WebP, or GIF; file size is positive and at most 10 MiB. Upload with the approximately 60-second service policy directly to GFS, register the returned GFS ID, and insert only the returned `MediaView.url` matching `/img/proxy/{publicKey}`.
- Preview is an authenticated SPA route, never creates a Release, never triggers Jenkins, and rewrites valid `/img/proxy/*` sources to `https://qiuxs.com/img/proxy/*`.
- Use the approved cold-engineering visual system: charcoal surfaces, cold-white copy, electric-blue focus/action color, restrained grid and borders, no hacker-green theme, character rain, or fault animation.
- Meet WCAG 2.2 AA: accessible names, keyboard operation, visible focus, associated errors, announced statuses, non-color status cues, and at least 44-by-44 CSS-pixel targets on narrow screens.
- Every route has explicit loading, empty, retryable failure, authorization loss, and successful-content states. Mutations block duplicates and retain a sanitized Problem title, code, and `requestId`.
- Automated Admin tests start no containers and connect to no real service, MySQL, Redis, Jenkins, GFS, or OSS.
- Production output is a static `admin/dist/` with `index.html`, hashed assets, no source maps, and no secrets. Stage 6 alone deploys it beneath `/web/deploy/blog-admin/releases/<revision>` with SPA fallback and atomic `current` switching.

---

## Stage Entry Contract

Do not begin Task 3 until Task 2 has established this mechanical checkpoint:

```bash
cd admin
test "$(node --version)" = "v20.19.4"
npm run generate:api
git diff --exit-code -- src/api/generated/admin.ts
npm run typecheck
```

Expected: generation succeeds, the second generation is byte-for-byte clean, and generated aliases plus `createAdminApi(options) satisfies AdminApi` compile. A capability mentioned by prose is not a browser API unless it appears below.

| Method and path | operationId | Success | Documented failures |
| --- | --- | --- | --- |
| `POST /api/admin/v1/session` | `loginAdmin` | `200 AdminView` | `400, 401, 403, 429, 503` |
| `DELETE /api/admin/v1/session` | `logoutAdmin` | `204` | `403, 503` |
| `GET /api/admin/v1/me` | `getCurrentAdmin` | `200 AdminView` | `401, 503` |
| `GET /api/admin/v1/articles` | `listArticles` | `200 ArticleList` | `400, 401, 503` |
| `POST /api/admin/v1/articles` | `createArticle` | `201 ArticleDetail` | `400, 401, 403, 409, 503` |
| `GET /api/admin/v1/articles/{articleId}` | `getArticle` | `200 ArticleDetail` | `400, 401, 404, 503` |
| `PUT /api/admin/v1/articles/{articleId}/draft` | `saveArticleDraft` | `200 DraftView` | `400, 401, 403, 404, 409, 422, 503` |
| `GET /api/admin/v1/articles/{articleId}/preview` | `getArticlePreview` | `200 PreviewView` | `400, 401, 404, 503` |
| `GET /api/admin/v1/articles/{articleId}/versions` | `listArticleVersions` | `200 RevisionList` | `400, 401, 404, 503` |
| `POST /api/admin/v1/articles/{articleId}/versions` | `createArticleVersion` | `201 VersionResult` | `400, 401, 403, 404, 409, 422, 503` |
| `POST /api/admin/v1/articles/{articleId}/versions/{revisionId}/restore` | `restoreArticleVersion` | `200 DraftView` | `400, 401, 403, 404, 409, 422, 503` |
| `POST /api/admin/v1/articles/{articleId}/trash` | `trashArticle` | `204` | `400, 401, 403, 404, 409, 503` |
| `POST /api/admin/v1/articles/{articleId}/untrash` | `untrashArticle` | `204` | `400, 401, 403, 404, 409, 503` |
| `GET /api/admin/v1/tags` | `listTags` | `200 TagList` | `400, 401, 503` |
| `POST /api/admin/v1/tags` | `createTag` | `201 TagView` | `400, 401, 403, 409, 503` |
| `PATCH /api/admin/v1/tags/{tagId}` | `renameTag` | `200 TagView` | `400, 401, 403, 404, 409, 503` |
| `POST /api/admin/v1/media/upload-policy` | `createMediaUploadPolicy` | `200 MediaUploadPolicy` | `400, 401, 403, 503` |
| `POST /api/admin/v1/media` | `registerMedia` | `201 MediaView` | `400, 401, 403, 409, 422, 503` |
| `GET /api/admin/v1/settings/site` | `getSiteSettings` | `200 SiteSettingsView` | `400, 401, 503` |
| `PUT /api/admin/v1/settings/site` | `putSiteSettings` | `200 SiteSettingsView` | `400, 401, 403, 409, 422, 503` |
| `GET /api/admin/v1/settings/hotlink` | `getHotlinkSettings` | `200 HotlinkSettingsView` | `400, 401, 503` |
| `PUT /api/admin/v1/settings/hotlink` | `putHotlinkSettings` | `200 HotlinkSettingsView` | `400, 401, 403, 409, 422, 503` |
| `GET /api/admin/v1/builder` | `getBuilderConfig` | `200 BuilderConfigView` | `400, 401, 404, 503` |
| `PUT /api/admin/v1/builder` | `putBuilderConfig` | `200 BuilderConfigView` | `400, 401, 403, 409, 422, 503` |
| `POST /api/admin/v1/builder/test` | `testBuilderConfig` | `204` | `400, 401, 403, 412, 503` |
| `GET /api/admin/v1/releases` | `listReleases` | `200 ReleaseList` | `400, 401, 503` |
| `POST /api/admin/v1/releases` | `createRelease` | `202 CreateReleaseResult` | `400, 401, 403, 409, 412, 503` |
| `GET /api/admin/v1/releases/{releaseId}` | `getRelease` | `200 ReleaseView` | `400, 401, 404, 503` |
| `POST /api/admin/v1/releases/{releaseId}/retry` | `retryRelease` | `202 RetryReleaseResult` | `400, 401, 403, 404, 409, 412, 503` |

Internal Bundle and Jenkins callback operations are not callable by the SPA and must not appear in `AdminApi` or browser mocks.

## Generated Type Aliases and Contract Invariants

`admin/src/api/admin-api.ts` exports aliases from generated types, not object-shaped replacements:

```ts
import type { components, operations } from "./generated/admin";

export type LoginRequest = components["schemas"]["LoginRequest"];
export type AdminView = components["schemas"]["AdminView"];
export type ArticleSummary = components["schemas"]["ArticleSummary"];
export type ArticleList = components["schemas"]["ArticleList"];
export type ArticleDetail = components["schemas"]["ArticleDetail"];
export type DraftView = components["schemas"]["DraftView"];
export type PreviewView = components["schemas"]["PreviewView"];
export type RevisionView = components["schemas"]["RevisionView"];
export type RevisionList = components["schemas"]["RevisionList"];
export type VersionResult = components["schemas"]["VersionResult"];
export type SaveDraftRequest = components["schemas"]["SaveDraftRequest"];
export type LockVersionRequest = components["schemas"]["LockVersionRequest"];
export type TagView = components["schemas"]["TagView"];
export type TagList = components["schemas"]["TagList"];
export type CreateTagRequest = components["schemas"]["CreateTagRequest"];
export type RenameTagRequest = components["schemas"]["RenameTagRequest"];
export type MediaUploadPolicy = components["schemas"]["MediaUploadPolicy"];
export type RegisterMediaRequest = components["schemas"]["RegisterMediaRequest"];
export type MediaView = components["schemas"]["MediaView"];
export type SiteSettingsView = components["schemas"]["SiteSettingsView"];
export type PutSiteSettingsRequest = components["schemas"]["PutSiteSettingsRequest"];
export type HotlinkSettingsView = components["schemas"]["HotlinkSettingsView"];
export type PutHotlinkSettingsRequest = components["schemas"]["PutHotlinkSettingsRequest"];
export type BuilderConfigView = components["schemas"]["BuilderConfigView"];
export type PutBuilderConfigRequest = components["schemas"]["PutBuilderConfigRequest"];
export type CreateReleaseRequest = components["schemas"]["CreateReleaseRequest"];
export type CreateReleaseResult = components["schemas"]["CreateReleaseResult"];
export type ReleaseView = components["schemas"]["ReleaseView"];
export type ReleaseList = components["schemas"]["ReleaseList"];
export type RetryReleaseResult = components["schemas"]["RetryReleaseResult"];
export type PublishJobView = components["schemas"]["PublishJobView"];
export type Problem = components["schemas"]["Problem"];
export type ListArticlesQuery = operations["listArticles"]["parameters"]["query"];
export type ListReleasesQuery = operations["listReleases"]["parameters"]["query"];
export type EntityId = number;
```

Fixtures and feature logic preserve these facts:

- `ArticleList` is `{ items }`; the only list filter is optional `state=active|trashed`. Article creation has no body. `ArticleSummary` uses `draftTitle` and has no release/job status fields.
- `ArticleDetail` contains its fields and `draft` directly. `DraftView.tags` is ordered `TagSnapshot[]`, `DraftView.media` is `MediaReference[]`, and saving sends ordered unique `tagIds`.
- Each tag snapshot has `tagId`, `name`, `slug`, and zero-based `position`. Each media reference has `mediaId`, `publicKey`, `purpose=cover|content`, and zero-based `position`; a draft contains at most 32 tags and 257 references (one optional cover plus 256 unique body images).
- Preview returns `{ slug, draft }`. Versions return `{ items }` without pagination; create takes `{ lockVersion }` and returns `{ version, draft }`; restore takes `{ lockVersion }` and returns the replacement draft.
- Trash and untrash return no body. Tags list as `{ items }` without query; create and rename accept `{ name }`.
- Upload-policy creation has no body and returns `uploadUrl`, `appId`, `policy`, `signature`, `timestamp`, `expire`, `nonce`, and `fileField`. Registration accepts only `gfsFileId` and `originalName`; `MediaView.url` is the stable Markdown URL.
- Site settings use nullable `id`/`updatedAt`, `lockVersion` starting at zero, `authorName`, `authorBio`, `seoDefaultTitle`, `seoDefaultDescription`, nullable `seoDefaultImageMediaId`, and read-only `filingUrl`. The PUT body excludes `id`, `updatedAt`, and `filingUrl`.
- Hotlink settings use `entries[{ hostname, enabled }]` with no row IDs or lock version.
- Builder config lives at `/api/admin/v1/builder`, exposes `tokenConfigured`, never exposes token material, and has no lock version. Its test operation succeeds with an empty `204` response.
- A Release request always contains `articleId`: positive for `publish_article`/`unpublish_article`, `null` for `publish_settings`. Creation returns `{ release, job }`.
- Release status is `queued|success|failed`; Job status is `pending|queued|building|deploying|success|failed`. Poll `ReleaseView.latestJob`, keep Release/Job IDs distinct, and retain the complete `jobs` retry history.
- Retry keeps the immutable Release ID and returns a new Job. No browser endpoint stops an active job.
- `PublishJobView.builderTarget` is a read-only non-secret snapshot containing name, HTTPS base URL, username, and job name.
- `PublishJobView.errorSummary` is a required, non-null string capped at 512 characters; `buildNumber` and `finishedAt` are nullable. Never reinterpret an empty error summary as missing contract data.
- Release responses contain no creation mode or article ID. A reloaded history view displays only contract-backed Release/job data and does not reconstruct intent.
- `Problem.code` is an unconstrained string, so the recognized set is deliberately non-exhaustive. Preserve it; recognize stable service values `unauthenticated`, `revision_conflict`, `settings_conflict`, `builder_conflict`, `release_conflict`, and `precondition_failed` only together with documented HTTP statuses. Every unknown code uses the generic sanitized Problem fallback.

## Shared Frontend Interfaces

```ts
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

export interface AdminApiOptions {
  fetch?: typeof fetch;
  onUnauthenticated?: () => void;
}

export class ApiProblem extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    readonly requestId: string,
    readonly title: string,
  );
}
```

## File Map

Tasks create `admin/` with focused `app`, `api`, `auth`, `components`, `layout`, `articles`, `editor`, `media`, `preview`, `versions`, `publishing`, `settings`, `styles`, `test`, and `e2e` directories. Shared Markdown CSS and fixtures live under `contracts/markdown/`. Root `.gitignore` ignores Admin dependencies/build/test artifacts, and root `README.md` links the Admin README. Each task below lists its exact files; do not change `service/`, `site/`, or `deploy/`.

### Task 1: Bootstrap the Exact-Toolchain SPA and Static Artifact Contract

**Files:**
- Create: `admin/.nvmrc`
- Create: `admin/package.json`
- Create: `admin/package-lock.json`
- Create: `admin/tsconfig.json`
- Create: `admin/vite.config.ts`
- Create: `admin/vitest.config.ts`
- Create: `admin/index.html`
- Create: `admin/scripts/require-node.mjs`
- Create: `admin/scripts/require-node.test.mjs`
- Create: `admin/scripts/verify-dist.mjs`
- Create: `admin/src/main.tsx`
- Create: `admin/src/test/setup.ts`
- Modify: `.gitignore`

**Interfaces:** Produces exact-version scripts, strict TypeScript/Vite/Vitest configuration, and static `admin/dist/`.

- [ ] **Step 1: Write the failing exact-version test**

```js
import test from "node:test";
import assert from "node:assert/strict";
import { assertNodeVersion } from "./require-node.mjs";

test("accepts only Node 20.19.4", () => {
  assert.doesNotThrow(() => assertNodeVersion("v20.19.4"));
  assert.throws(() => assertNodeVersion("v20.19.3"), /Node 20\.19\.4 required/);
  assert.throws(() => assertNodeVersion("v22.20.0"), /Node 20\.19\.4 required/);
});
```

- [ ] **Step 2: Verify red**

Run: `cd admin && node --test scripts/require-node.test.mjs`

Expected: FAIL because `require-node.mjs` is absent.

- [ ] **Step 3: Add the toolchain and scripts**

Set `.nvmrc` and `engines.node` to `20.19.4`; configure strict TypeScript with `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`, and bundler resolution. Use these scripts:

```json
{
  "check:node": "node scripts/require-node.mjs",
  "dev": "npm run check:node && vite",
  "preview": "vite preview",
  "generate:api": "openapi-typescript ../contracts/openapi/admin-v1.yaml -o src/api/generated/admin.ts",
  "typecheck": "tsc --noEmit",
  "test": "vitest",
  "test:run": "vitest run",
  "build": "npm run check:node && npm run typecheck && vite build",
  "verify:dist": "node scripts/verify-dist.mjs"
}
```

Install and lock React, React Router, TanStack Query, openapi-fetch/typescript, Milkdown, unified/remark/rehype/Shiki, and Vitest dependencies. Do not add React Testing Library, jest-axe, MSW, or Playwright solely for UI verification. Disable source maps and proxy `/api` to `http://127.0.0.1:8080` in development without rewriting Host or Origin.

```bash
cd admin
npm install react react-dom react-router-dom @tanstack/react-query openapi-fetch @milkdown/kit @milkdown/react unified remark-parse remark-gfm remark-rehype rehype-sanitize rehype-stringify @shikijs/rehype github-slugger
npm install --save-dev typescript vite @vitejs/plugin-react openapi-typescript vitest @types/react @types/react-dom @types/node
```

- [ ] **Step 4: Verify green and artifact shape**

Run:

```bash
cd admin
test "$(node --version)" = "v20.19.4"
npm install
node --test scripts/require-node.test.mjs
npm run typecheck
npm run build
npm run verify:dist
```

Expected: PASS; `dist/index.html` and hashed JS/CSS exist, no `.map` or exact protected identifier from Task 16 exists, and output stays ignored. The identifier scan is deliberately limited to the seven concrete deployment names; it does not reject ordinary UI/source text merely because it contains `token`, `password`, or `secret`.

- [ ] **Step 5: Commit**

```bash
git add .gitignore admin
git commit -m "chore(admin): bootstrap react workspace"
```

### Task 2: Generate Types and Build the Sanitized API Boundary

**Files:**
- Create: `admin/src/api/generated/admin.ts`
- Create: `admin/src/api/admin-api.ts`
- Create: `admin/src/api/admin-api.test.ts`
- Create: `admin/src/api/ids.ts`
- Create: `admin/src/api/problem.ts`
- Create: `admin/src/api/query-keys.ts`
- Create: `admin/src/test/fixtures.ts`
- Create: `admin/src/test/handlers.ts`
- Create: `admin/src/test/server.ts`
- Create: `admin/src/test/render.tsx`
- Modify: `admin/src/test/setup.ts`

**Interfaces:** Consumes all 29 Stage Entry operations. Produces `createAdminApi(options?): AdminApi`, generated aliases, `ApiProblem`, ID guards, query keys, and exact fixtures.

- [ ] **Step 1: Generate types and write failing boundary tests**

Run `cd admin && npm run generate:api`, then test exact URL/method/body/status handling. Include compile-time checks:

```ts
const api = createAdminApi() satisfies AdminApi;
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

await api.createArticle();
await api.createMediaUploadPolicy();
await api.testBuilderConfig();
```

Assert those three calls send no JSON body, each 204 operation resolves `undefined`, a `401 application/problem+json` becomes `ApiProblem`, unsafe/invalid returned IDs are rejected, and submitted password/token values never appear in errors. Preserve `builder_conflict` and an arbitrary `future_service_code` verbatim in `ApiProblem`; neither transport mapping nor presentation may treat the known-code list as exhaustive.

- [ ] **Step 2: Verify red**

Run: `cd admin && npm test -- --run src/api/admin-api.test.ts`

Expected: FAIL because the adapter and fixtures are absent.

- [ ] **Step 3: Implement all exact operation mappings**

Use `createClient<paths>({ baseUrl: window.location.origin, credentials: "include", fetch })`. Implement the Stage Entry table exactly: session/me; article list/create/detail/draft/preview/version/restore/trash/untrash; tag list/create/rename; media policy/register; site/hotlink get/put; builder get/put/test; and release list/create/get/retry. Path IDs go through `requireEntityId`; inputs pass generated bodies/query without field renaming. `unwrapVoid` accepts only documented empty success, and `unwrap` accepts only documented success data.

```ts
export function requireEntityId(value: unknown, field: string): EntityId {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value <= 0) {
    throw new ApiProblem(502, "invalid_api_response", "client", `Invalid ${field}`);
  }
  return value;
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
```

Only a `401` Problem with code `unauthenticated` calls `onUnauthenticated`. Network and invalid-response failures use fixed client Problems without raw response text or request data.

- [ ] **Step 4: Add exact typed fixtures and query keys**

Build fixtures with generated aliases; these objects are the canonical baseline that feature tests copy and override:

```ts
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
```

Define hierarchical keys with `queryKeys.articlesRoot = ["articles"]` and article-list/detail/preview/version keys below that root. Define `queryKeys.releasesRoot = ["releases"]`, `queryKeys.releaseListsRoot = ["releases", "list"]`, every limit/offset list below `releaseListsRoot`, and `queryKeys.release(id) = ["releases", "detail", id]`. Also define me, tags, site, hotlink, and builder keys. Invalidating `articlesRoot` covers every article list and detail; invalidating `releaseListsRoot` covers every Release list without making a freshly seeded Release detail stale. MSW throws on every unhandled request.

- [ ] **Step 5: Verify generation and complete operation coverage**

```bash
cd admin
npm test -- --run src/api/admin-api.test.ts
npm run typecheck
npm run generate:api
git diff --exit-code -- src/api/generated/admin.ts
```

Expected: PASS; tests exercise every `AdminApi` member and the second generation is clean.

- [ ] **Step 6: Commit**

```bash
git add admin/src/api admin/src/test admin/package.json admin/package-lock.json
git commit -m "feat(admin): add generated api client"
```

### Task 3: Establish the Accessible Cold-Engineering Shell

**Files:**
- Create: `admin/src/app/AppProviders.tsx`
- Create: `admin/src/app/AppRouter.tsx`
- Create: `admin/src/app/RouteErrorPage.tsx`
- Create: `admin/src/layout/AppShell.tsx`
- Create: `admin/src/layout/AppShell.test.tsx`
- Create: `admin/src/components/AsyncPage.tsx`
- Create: `admin/src/components/ConfirmDialog.tsx`
- Create: `admin/src/components/FormField.tsx`
- Create: `admin/src/components/ProblemNotice.tsx`
- Create: `admin/src/components/SaveIndicator.tsx`
- Create: `admin/src/components/StatusBadge.tsx`
- Create: `admin/src/styles/tokens.css`
- Create: `admin/src/styles/base.css`
- Create: `admin/src/styles/components.css`
- Modify: `admin/src/main.tsx`

**Interfaces:** Consumes `ApiProblem`, QueryClient, and React Router. Produces providers, shell, dialogs, form/error/status primitives, and responsive navigation.

- [ ] **Step 1: Write failing semantic and accessibility tests**

```tsx
it("exposes one keyboard-navigable shell", async () => {
  renderApp(<AppShell><h1>Articles</h1></AppShell>);
  expect(screen.getByRole("link", { name: "Skip to content" })).toHaveAttribute("href", "#main-content");
  expect(screen.getByRole("navigation", { name: "Admin" })).toBeInTheDocument();
  expect(screen.getByRole("main")).toHaveAttribute("id", "main-content");
  expect(await axe(document.body)).toHaveNoViolations();
});
```

Cover desktop links Articles/Publishing/Site/Builder/Hotlink, mobile `aria-expanded`, focus return, announced loading/save states, sanitized Problem/request ID, confirmation focus trap, and 44px narrow-screen targets. Add one `builder_conflict` presentation case and one `future_service_code` case; the latter must render the generic title/code/request ID without an empty state, exception, or specialized claim.

- [ ] **Step 2: Verify red**

Run: `cd admin && npm test -- --run src/layout/AppShell.test.tsx`

Expected: FAIL because shell components do not exist.

- [ ] **Step 3: Build providers, primitives, and visual tokens**

Use one QueryClient: mutations never retry; queries retry once except 401; dirty editor routes suppress focus refetch. Use semantic header/nav/main, route-aware `aria-current`, keyboard-closeable drawer, skip link, and safe route errors. Define `#0d1117`, `#131a23`, `#eef4fb`, `#9eacbd`, and `#2f9cff` core tokens, visible focus, non-color status icons, reduced motion, 72rem shell max, and a 48rem narrow breakpoint.

- [ ] **Step 4: Verify green**

Run: `cd admin && npm test -- --run src/layout/AppShell.test.tsx && npm run typecheck`

Expected: PASS for semantics, keyboard, responsive controls, Problem rendering, and axe.

- [ ] **Step 5: Commit**

```bash
git add admin/src/app admin/src/layout admin/src/components admin/src/styles admin/src/main.tsx
git commit -m "feat(admin): add accessible application shell"
```

### Task 4: Implement Session Bootstrap and Protected Routing

**Files:**
- Create: `admin/src/auth/AuthProvider.tsx`
- Create: `admin/src/auth/RequireSession.tsx`
- Create: `admin/src/auth/LoginPage.tsx`
- Create: `admin/src/auth/auth.test.tsx`
- Modify: `admin/src/app/AppProviders.tsx`
- Modify: `admin/src/app/AppRouter.tsx`
- Modify: `admin/src/layout/AppShell.tsx`

**Interfaces:** Consumes `loginAdmin`, `logoutAdmin`, `getCurrentAdmin`, `AdminView`, and `ApiProblem`. Produces auth context, `/login`, protected routes, and session-loss handling.

- [ ] **Step 1: Write failing auth-route tests**

Cover initial me 200, me 401 preserving intended pathname, me 503 retry, successful login, invalid credentials retaining username/clearing password, logout 204, and central session expiry. Assert no password or cookie value appears in storage or URL.

- [ ] **Step 2: Verify red**

Run: `cd admin && npm test -- --run src/auth/auth.test.tsx`

Expected: FAIL because auth modules are absent.

- [ ] **Step 3: Implement auth state and exact routes**

```ts
export interface AuthContextValue {
  api: AdminApi;
  state:
    | { kind: "loading" }
    | { kind: "anonymous" }
    | { kind: "authenticated"; admin: AdminView }
    | { kind: "unavailable"; problem: ApiProblem };
  login(input: LoginRequest): Promise<void>;
  logout(): Promise<void>;
  retry(): void;
}
```

Construct one API with `onUnauthenticated`, call `getCurrentAdmin()` once, call `loginAdmin(input)`/`logoutAdmin()`, and define `/login`, `/articles`, `/articles/new`, article edit/preview/versions, `/publishing`, and three settings routes. Protected routes render only after auth; 503 shows dependency retry rather than login.

- [ ] **Step 4: Harden form and session loss**

Use `autocomplete="username"` and `autocomplete="current-password"`, disable duplicates, focus announced error, and never copy `Set-Cookie`. Central expiry clears auth/query state after editor navigation protection has preserved local recovery data.

- [ ] **Step 5: Verify green**

```bash
cd admin
npm test -- --run src/auth/auth.test.tsx
npm run typecheck
```

Expected: PASS for authenticated, anonymous, unavailable, invalid-credential, session-expiry, and logout states.

- [ ] **Step 6: Commit**

```bash
git add admin/src/auth admin/src/app admin/src/layout
git commit -m "feat(admin): add session protected routing"
```

### Task 5: Deliver the Contract-Backed Article List and Lifecycle

**Files:**
- Create: `admin/src/articles/ArticleListPage.tsx`
- Create: `admin/src/articles/ArticleListPage.test.tsx`
- Create: `admin/src/articles/article-actions.ts`
- Create: `admin/src/publishing/release-cache.ts`
- Create: `admin/src/publishing/release-cache.test.ts`
- Modify: `admin/src/app/AppRouter.tsx`
- Modify: `admin/src/api/query-keys.ts`

**Interfaces:** Consumes `listArticles`, bodyless `createArticle`, void `trashArticle`/`untrashArticle`, `createRelease`, `ArticleSummary`, and hierarchical query keys. Produces active/trash views, safe lifecycle actions, and `syncReleaseCache(queryClient, release, source): Promise<void>` for every later Release caller, where source is `create`, `retry`, or `poll`.

- [ ] **Step 1: Write failing list/lifecycle tests**

Cover loading, empty, retryable failure, exact `draftTitle`/timestamps/state, active and trashed URL filters, bodyless creation, edit navigation, unpublished trash, published trash blocked, untrash 204, unpublish Release creation, duplicate-action disabling, and narrow action menu.

In `release-cache.test.ts`, prove every Release result seeds `queryKeys.release(release.id)`. A `create` source invalidates all queries under `queryKeys.releaseListsRoot` exactly once without invalidating the seeded detail. A `retry` or `poll` source uses `setQueriesData` to immutably replace the matching Release in every cached list and never invalidates/refetches a Release list. Every source with `status === "success"` additionally invalidates the whole `queryKeys.articlesRoot`, covering all article lists and details without needing an article ID or Release mode.

```tsx
await user.click(screen.getByRole("button", { name: "Confirm unpublish" }));
expect(createRelease).toHaveBeenCalledWith({ mode: "unpublish_article", articleId: 11 });
```

- [ ] **Step 2: Verify red**

Run: `cd admin && npm test -- --run src/articles/ArticleListPage.test.tsx src/publishing/release-cache.test.ts`

Expected: FAIL because the list, actions, and Release cache helper are absent.

- [ ] **Step 3: Implement exact query/display semantics**

Accept only `?state=active` or `?state=trashed`; invalid/missing becomes active. Send no page/query extras, keep service order, use `draftTitle` with a UI-only “Untitled draft” fallback, and derive only current online state from `publishedRevisionId !== null`. Do not render a last release result because ArticleSummary does not contain one; link to Publishing for release history.

- [ ] **Step 4: Implement bodyless lifecycle calls**

`createArticle()` validates `detail.id` and navigates to edit. Confirm trash/untrash, call the void operations, then invalidate list/detail queries. Block trash when `publishedRevisionId` is non-null. Implement the shared cache rule exactly:

```ts
export type ReleaseCacheSource = "create" | "retry" | "poll";

export async function syncReleaseCache(
  queryClient: QueryClient,
  release: ReleaseView,
  source: ReleaseCacheSource,
): Promise<void> {
  const releaseId = requireEntityId(release.id, "release.id");
  queryClient.setQueryData(queryKeys.release(releaseId), release);

  const invalidations: Promise<void>[] = [];
  if (source === "create") {
    invalidations.push(queryClient.invalidateQueries({ queryKey: queryKeys.releaseListsRoot }));
  } else {
    queryClient.setQueriesData<ReleaseList>(
      { queryKey: queryKeys.releaseListsRoot },
      (current) => {
        if (current === undefined) return current;
        let matched = false;
        const items = current.items.map((item) => {
          if (item.id !== releaseId) return item;
          matched = true;
          return release;
        });
        return matched ? { ...current, items } : current;
      },
    );
  }
  if (release.status === "success") {
    invalidations.push(queryClient.invalidateQueries({ queryKey: queryKeys.articlesRoot }));
  }
  await Promise.all(invalidations);
}
```

Unpublish calls `createRelease({ mode: "unpublish_article", articleId })`, awaits `syncReleaseCache(queryClient, result.release, "create")`, then navigates to `/publishing?release=<release.id>` without claiming the article is offline before Release success.

- [ ] **Step 5: Verify green**

```bash
cd admin
npm test -- --run src/articles/ArticleListPage.test.tsx src/publishing/release-cache.test.ts
npm run typecheck
```

Expected: PASS for both real filters, exact bodyless/void mutations, published-state guard, and Release handoff.

- [ ] **Step 6: Commit**

```bash
git add admin/src/articles admin/src/publishing/release-cache.ts admin/src/publishing/release-cache.test.ts admin/src/app/AppRouter.tsx admin/src/api/query-keys.ts
git commit -m "feat(admin): add article lifecycle list"
```

### Task 6: Build Milkdown Editing, Exact Metadata, and Tag Operations

**Files:**
- Create: `admin/src/editor/editor-document.ts`
- Create: `admin/src/editor/editor-document.test.ts`
- Create: `admin/src/editor/milkdown-adapter.ts`
- Create: `admin/src/editor/MarkdownEditor.tsx`
- Create: `admin/src/editor/ArticleEditorPage.tsx`
- Create: `admin/src/editor/ArticleEditorPage.test.tsx`
- Create: `admin/src/styles/editor.css`
- Modify: `admin/src/app/AppRouter.tsx`

**Interfaces:** Consumes `getArticle`, bodyless `createArticle`, `listTags`, `createTag`, `renameTag`, `ArticleDetail`, and `SaveDraftRequest`. Produces visual/source editor state with ordered tag IDs.

```ts
export interface EditorDocument {
  title: string;
  summary: string;
  coverMediaId: EntityId | null;
  contentMd: string;
  tagIds: EntityId[];
}
```

- [ ] **Step 1: Write failing document/editor tests**

Test conversion from `ArticleDetail.draft`, ordered `TagSnapshot.tagId` extraction by position, unique selected IDs, exact save body, new route creation without a request body, loading/failure, collapsed metadata, visual default, source synchronization, whole-GFM paste, tag list `{items}`, create/rename `{name}`, and 32-tag limit.

```ts
expect(toSaveRequest(document, 7)).toEqual({
  lockVersion: 7,
  title: "Build log",
  summary: "Summary",
  coverMediaId: null,
  contentMd: "# Build log\n",
  tagIds: [31, 32],
});
```

- [ ] **Step 2: Verify red**

Run: `cd admin && npm test -- --run src/editor/editor-document.test.ts src/editor/ArticleEditorPage.test.tsx`

Expected: FAIL because editor modules are absent.

- [ ] **Step 3: Implement validation and request construction**

Title may be empty for recoverable autosave but is capped at 200 code points; summary is capped at 600; tag IDs are positive, ordered, unique, and at most 32. Validate created/renamed tag names as 1–64 code points. Reject `blob:` content for version/publish; retain invalid local input but do not send it. Measure raw Markdown and `JSON.stringify(SaveDraftRequest)` as UTF-8 and keep both below the service 2 MiB boundaries.

- [ ] **Step 4: Implement Milkdown and exact route load**

Use CommonMark plus GFM table/task/strikethrough only, listener as ordinary visual-to-string path, exact whole-document plain-text paste into an empty canvas, no HTML/footnotes plugins, and clean mode unmount. Edit loads `getArticle(id)`; new calls `createArticle()` once and replaces navigation with its validated ID. Title is always visible; summary/tags/cover are in `<details>`.

- [ ] **Step 5: Implement tag selection/create/rename**

Load `listTags()` and render `result.items`; select by ID. Create and rename send only `{ name }`, replace/invalidate the tag cache from returned `TagView`, and keep draft selection by stable ID. Do not submit query text to tag listing or tag names in draft saves.

- [ ] **Step 6: Verify green**

```bash
cd admin
npm test -- --run src/editor/editor-document.test.ts src/editor/ArticleEditorPage.test.tsx
npm run typecheck
```

Expected: PASS for exact ArticleDetail load, tag-ID saves, limits, GFM paste, and source synchronization.

- [ ] **Step 7: Commit**

```bash
git add admin/src/editor admin/src/styles/editor.css admin/src/app/AppRouter.tsx admin/package.json admin/package-lock.json
git commit -m "feat(admin): add milkdown article editor"
```

### Task 7: Add Race-Safe Two-Second Autosave and Conflict Recovery

**Files:**
- Create: `admin/src/editor/useAutosave.ts`
- Create: `admin/src/editor/useAutosave.test.tsx`
- Create: `admin/src/editor/ConflictDialog.tsx`
- Modify: `admin/src/editor/ArticleEditorPage.tsx`
- Modify: `admin/src/components/SaveIndicator.tsx`

**Interfaces:** Consumes `saveArticleDraft`, `getArticle`, `DraftView.lockVersion`, and `ApiProblem`. Produces serialized autosave, recovery, and dirty navigation protection.

```ts
export type SaveState =
  | { kind: "saved"; savedAt: Date; lockVersion: number }
  | { kind: "dirty"; lockVersion: number }
  | { kind: "saving"; lockVersion: number }
  | { kind: "failed"; lockVersion: number; problem: ApiProblem }
  | { kind: "conflict"; lockVersion: number; local: EditorDocument };

export interface AutosaveOptions {
  articleId: EntityId;
  initial: EditorDocument;
  initialLockVersion: number;
  delayMs: 2000;
  save(input: SaveDraftRequest): Promise<DraftView>;
  reload(): Promise<ArticleDetail>;
}
```

- [ ] **Step 1: Write failing fake-timer tests**

Prove no call at 1,999 ms, one at 2,000, edit resets timer, invalid/envelope-oversize input remains local, one request at a time, in-flight edit uses returned lock version, stale response never replaces newer Markdown, retryable failure, `409 revision_conflict`, copy/reload, successful saved timestamp, and unmount cleanup.

- [ ] **Step 2: Verify red**

Run: `cd admin && npm test -- --run src/editor/useAutosave.test.tsx`

Expected: FAIL because the hook and dialog are absent.

- [ ] **Step 3: Implement generation-aware serialization**

Track local generation and one in-flight promise. Capture generation/document/lock for each request. Adopt returned `DraftView.lockVersion`; mark saved only if generation remains current, otherwise schedule the newest valid document immediately. Enter conflict only for status 409 plus `revision_conflict`; other 409/failures remain visible generic failures.

- [ ] **Step 4: Implement recovery and navigation guard**

Announce Saving/Saved/Save failed/Version conflict. Copy only `local.contentMd`; confirmed reload calls `getArticle`, then replaces all fields from `detail.draft`. Use `beforeunload` and a Router blocker whenever state is not saved. Disable Preview, Versions, Create Version, and Publish until saved.

- [ ] **Step 5: Verify green**

```bash
cd admin
npm test -- --run src/editor/useAutosave.test.tsx src/editor/ArticleEditorPage.test.tsx
npm run typecheck
```

Expected: PASS for debounce, serialization, in-flight edits, retry, revision conflict, recovery, and navigation guard.

- [ ] **Step 6: Commit**

```bash
git add admin/src/editor admin/src/components/SaveIndicator.tsx
git commit -m "feat(admin): add conflict safe autosave"
```

### Task 8: Implement Direct GFS Upload and Stable Media Registration

**Files:**
- Create: `admin/src/media/image-upload.ts`
- Create: `admin/src/media/image-upload.test.ts`
- Create: `admin/src/media/useEditorImageUpload.ts`
- Modify: `admin/src/editor/MarkdownEditor.tsx`
- Modify: `admin/src/editor/ArticleEditorPage.tsx`

**Interfaces:** Consumes bodyless `createMediaUploadPolicy`, the GFS upload envelope, `registerMedia({ gfsFileId, originalName })`, and `MediaView.url`. Produces progress/cancel and image insertion.

```ts
export interface UploadImageDependencies {
  api: Pick<AdminApi, "createMediaUploadPolicy" | "registerMedia">;
  sendMultipart(
    policy: MediaUploadPolicy,
    file: File,
    onProgress: (percent: number) => void,
    signal: AbortSignal,
  ): Promise<EntityId>;
}
```

- [ ] **Step 1: Write failing chain tests**

Reject unsupported MIME/extension pairs, zero bytes, and over 10 MiB. Reject `file.name` before any policy request when it contains `/`, `\\`, or NUL, equals `.` or `..`, or exceeds 255 UTF-8 bytes; assert `createMediaUploadPolicy` is not called for every such case. Assert policy call has no body; multipart uses `uploadUrl`, fields `appId`, `policy`, `signature`, `timestamp`, `expire`, `nonce`, then file under `fileField`; `code:0` and positive numeric `data.val` yield a GFS ID; registration body is exactly `{ gfsFileId, originalName }`; abort skips registration; errors omit URL/policy/signature; insertion uses returned `media.url` only.

Treat upload-envelope width/height strings as diagnostic only: the Admin does not send them to registration, and the service metadata lookup authoritatively enforces positive dimensions no greater than 12,000 pixels.

```ts
expect(registerMedia).toHaveBeenCalledWith({ gfsFileId: 41, originalName: "photo.png" });
expect(insertMarkdown).toHaveBeenCalledWith("![photo](/img/proxy/m_abcdefghijklmnopqrstuv)");
```

- [ ] **Step 2: Verify red**

Run: `cd admin && npm test -- --run src/media/image-upload.test.ts`

Expected: FAIL because upload orchestration is absent.

- [ ] **Step 3: Implement exact multipart transport**

Use XMLHttpRequest for progress, FormData boundary set by the browser, and abort via `xhr.abort()`. Validate policy strings, `fileField === "file"`, decimal timestamp, `expire === "60"`, and expiry still in the future before upload. Parse only HTTP 200 JSON with `code === 0` and safe positive integer `data.val`; ignore `objectInfo` for registration because the service verifies metadata itself.

- [ ] **Step 4: Implement registration and editor integration**

Run the filename/MIME/size preflight synchronously before `createMediaUploadPolicy()`. Only after it passes, request the policy, upload, then call `registerMedia({ gfsFileId, originalName: file.name })`. Validate returned ID/public key and `url` against `^/img/proxy/[a-z0-9_-]+$`. Paste/drop/picker and cover share the hook; body inserts escaped alt plus `media.url`, cover stores `media.id`; preflight or upload failure leaves the document unchanged.

- [ ] **Step 5: Verify green**

```bash
cd admin
npm test -- --run src/media/image-upload.test.ts src/editor/ArticleEditorPage.test.tsx
npm run typecheck
```

Expected: PASS for filename/MIME/extension/size preflight before the policy call, exact policy multipart, GFS ID parsing, registration, abort, and stable URL insertion.

- [ ] **Step 6: Commit**

```bash
git add admin/src/media admin/src/editor
git commit -m "feat(admin): add direct image upload"
```

### Task 9: Create the Safe Full-Article Preview and Markdown Contract

**Files:**
- Create: `contracts/markdown/article-content.css`
- Create: `contracts/markdown/fixtures/full-gfm.md`
- Create: `admin/src/preview/render-markdown.ts`
- Create: `admin/src/preview/render-markdown.test.ts`
- Create: `admin/src/preview/ArticlePreviewPage.tsx`
- Create: `admin/src/preview/ArticlePreviewPage.test.tsx`
- Modify: `admin/src/app/AppRouter.tsx`
- Modify: `admin/vite.config.ts`

**Interfaces:** Consumes `getArticlePreview`, `PreviewView`, approved Markdown, and public origin. Produces protected preview and shared Stage 5 fixture/style contract.

- [ ] **Step 1: Write failing renderer and route tests**

The fixture contains every supported syntax, duplicate h2 headings, fenced Go, external HTTPS link, `/img/proxy/m_fixturepublickey`, raw script/event content, and a JavaScript URL. Assert safe GFM, stable duplicate IDs, safe external attributes, public-origin image rewrite, Shiki output, immutable source input, loading/failure, and rendering from `PreviewView.draft`. Add negative image cases for an empty key and valid-looking paths followed by a suffix, query, or fragment; none may be rewritten or retained as an image source.

- [ ] **Step 2: Verify red**

Run: `cd admin && npm test -- --run src/preview/render-markdown.test.ts src/preview/ArticlePreviewPage.test.tsx`

Expected: FAIL because preview files are absent.

- [ ] **Step 3: Implement the safe pipeline**

Use remark parse/GFM/rehype without dangerous HTML, explicit sanitize schema, deterministic slugger, external-link transform, and an exact `^/img/proxy/[a-z0-9_-]+$` image-source match before rewriting to `https://qiuxs.com`. Reject `/img/proxy/`, `/img/proxy/key/suffix`, `/img/proxy/key?x=1`, and `/img/proxy/key#fragment`; remove their image sources rather than partially matching. Use Shiki allowlisted grammars and stringify. Do not use raw-HTML parsing. Extract h2/h3 table-of-contents entries.

- [ ] **Step 4: Implement the protected route**

Validate route ID and call `getArticlePreview(id)`. Render `preview.slug` and saved `preview.draft` title/summary/tags/media/body; do not read an editor singleton or cause a save/Release. A directly opened route shows the server draft. Lazy-load renderer and use shared `contracts/markdown/article-content.css` via a read-only Vite filesystem allowance.

- [ ] **Step 5: Verify green**

```bash
cd admin
npm test -- --run src/preview/render-markdown.test.ts src/preview/ArticlePreviewPage.test.tsx
npm run typecheck
npm run build
npm run verify:dist
```

Expected: PASS with inert dangerous input, public image origin, contract PreviewView data, and a lazy preview chunk.

- [ ] **Step 6: Commit**

```bash
git add contracts/markdown admin/src/preview admin/src/app/AppRouter.tsx admin/vite.config.ts admin/package.json admin/package-lock.json
git commit -m "feat(admin): add safe article preview"
```

### Task 10: Add Immutable Version History and Copy-on-Restore

**Files:**
- Create: `admin/src/versions/ArticleVersionsPage.tsx`
- Create: `admin/src/versions/ArticleVersionsPage.test.tsx`
- Modify: `admin/src/editor/ArticleEditorPage.tsx`
- Modify: `admin/src/app/AppRouter.tsx`
- Modify: `admin/src/api/query-keys.ts`

**Interfaces:** Consumes `listArticleVersions`, `createArticleVersion`, `restoreArticleVersion`, current `DraftView.lockVersion`, `RevisionList`, `VersionResult`, and replacement `DraftView`. Produces immutable history and cache/refetch behavior.

- [ ] **Step 1: Write failing history tests**

Cover `{items}` loading/empty/failure, service ordering, full `RevisionView` fields, manual/publish snapshot labels, no pagination, create disabled unless saved, create body `{lockVersion}`, returned version plus draft, restore confirmation and `{lockVersion}`, restore returning only a draft, and unchanged history rows.

```ts
expect(createArticleVersion).toHaveBeenCalledWith(11, { lockVersion: 7 });
expect(restoreArticleVersion).toHaveBeenCalledWith(11, 41, { lockVersion: 8 });
```

- [ ] **Step 2: Verify red**

Run: `cd admin && npm test -- --run src/versions/ArticleVersionsPage.test.tsx`

Expected: FAIL because version UI is absent.

- [ ] **Step 3: Implement history and exact mutation results**

Render `RevisionList.items` in delivered order with title, summary, ordered tag snapshots, timestamps, reason, and immutable content affordance. Validate IDs; do not infer chronology from IDs. Create uses the current saved draft lock; use `VersionResult.draft`, invalidate/refetch ArticleDetail so its draft pointer is authoritative, and invalidate history. Restore passes the current editing lock, accepts returned DraftView, invalidates/refetches ArticleDetail, and navigates to edit. Never mutate a RevisionView.

- [ ] **Step 4: Verify green**

```bash
cd admin
npm test -- --run src/versions/ArticleVersionsPage.test.tsx src/editor/ArticleEditorPage.test.tsx
npm run typecheck
```

Expected: PASS for full immutable rows, exact lock bodies, VersionResult handling, DraftView restore, and cache refetch.

- [ ] **Step 5: Commit**

```bash
git add admin/src/versions admin/src/editor/ArticleEditorPage.tsx admin/src/app/AppRouter.tsx admin/src/api/query-keys.ts
git commit -m "feat(admin): add article version history"
```

### Task 11: Implement Release Creation, History, Polling, and Retry

**Files:**
- Create: `admin/src/publishing/release-status.ts`
- Create: `admin/src/publishing/PublishingPage.tsx`
- Create: `admin/src/publishing/PublishingPage.test.tsx`
- Modify: `admin/src/publishing/release-cache.ts`
- Modify: `admin/src/publishing/release-cache.test.ts`
- Modify: `admin/src/editor/ArticleEditorPage.tsx`
- Modify: `admin/src/articles/ArticleListPage.tsx`
- Modify: `admin/src/app/AppRouter.tsx`
- Modify: `admin/src/api/query-keys.ts`

**Interfaces:** Consumes `createRelease`, `listReleases`, `getRelease`, `retryRelease`, `ReleaseView`, `PublishJobView`, autosave state, and `syncReleaseCache`. Produces article/settings Release actions, offset history, latest-job polling, retry history, and consistent Release/article cache invalidation.

```ts
export function isActiveJobStatus(status: PublishJobView["status"]): boolean {
  return status === "pending" || status === "queued" || status === "building" || status === "deploying";
}

export function releaseStatusLabel(status: ReleaseView["status"]): string;
export function jobStatusLabel(status: PublishJobView["status"]): string;
```

- [ ] **Step 1: Write failing Release/job state tests**

Cover all three Release statuses and all six Job statuses; never pass one status union to the other's label function. Cover list request `{limit:20, offset:0}`, no total/page fields, previous/next offset behavior, selected Release query parameter, three-second polling through `getRelease(releaseId)` only while `latestJob` is active, stop on terminal latest job/unmount, full `jobs` retry history, checksum/completedAt, safe errorSummary, build number, finishedAt, and read-only builder target.

```ts
expect(createRelease).toHaveBeenCalledWith({ mode: "publish_article", articleId: 11 });
expect(createRelease).toHaveBeenCalledWith({ mode: "publish_settings", articleId: null });
expect(retryRelease).toHaveBeenCalledWith(71);
```

Assert retry result keeps `release.id === 71`, returns a different `job.id`, makes returned `release.latestJob` equal the new job, and retains older jobs. Assert the UI has no control for an unsupported active-job mutation.

For create, assert the returned Release is immediately readable from its detail key and every Release-list key is invalidated once. For retry and each poll response, assert detail replacement plus immutable matching-row updates through `setQueriesData`, with no Release-list invalidation. Start one active list query, deliver at least two detail poll responses with fake timers, and assert the `listReleases` request count remains exactly one while the cached row advances. For any returned successful Release, assert broad `articlesRoot` invalidation; assert no cache path reads mode/article ID from ReleaseView.

- [ ] **Step 2: Verify red**

Run: `cd admin && npm test -- --run src/publishing/PublishingPage.test.tsx src/publishing/release-cache.test.ts`

Expected: FAIL because Release UI and status functions are absent.

- [ ] **Step 3: Implement list, selection, and polling**

Call `listReleases({ limit, offset })`; render only `ReleaseList.items`. Enable Previous when offset is positive and Next only when the current item count equals limit. Selection uses `/publishing?release=<positive id>`. Poll `getRelease(id)` every 3,000 ms while `isActiveJobStatus(release.latestJob.status)`, await `syncReleaseCache(queryClient, polledRelease, "poll")` on every response, and stop for success/failed latest jobs. The list query itself never polls, and detail polling never invalidates or refetches it.

Render Release ID, Release status, checksum, created/completed timestamps, then latest Job and all retry attempts. Keep `release.id`, `job.id`, and `job.releaseId` visibly distinct. Show `builderTarget.name/baseUrl/username/jobName` as non-editable text/link and never infer a token. Since ReleaseView exposes no creation mode or article ID, a reloaded row is labelled only by its Release-backed data.

Use exact non-color labels: Release `queued` → “Release queued”, `success` → “Release published”, `failed` → “Release failed”; Job `pending` → “Trigger pending”, `queued` → “Jenkins queued”, `building` → “Building”, `deploying` → “Deploying”, `success` → “Succeeded”, and `failed` → “Failed”.

- [ ] **Step 4: Implement creation and retry actions**

Editor Publish is enabled only for a saved draft with a nonblank title and no `blob:`. It calls `createRelease({ mode: "publish_article", articleId })`; unpublish continues to use the matching request from Task 5; the Publishing page's “Publish saved site settings” calls `createRelease({ mode: "publish_settings", articleId: null })`. Every creation caller awaits `syncReleaseCache(queryClient, result.release, "create")` before selecting the validated Release ID. Do not claim public success until Release/Job success is returned.

Retry is available for a failed aggregate/latest job and calls `retryRelease(release.id)`. Await `syncReleaseCache(queryClient, result.release, "retry")` and focus `result.job`. On `409 release_conflict`, show the generic global-serialization notice and keep the current selection; on `412 precondition_failed`, state that service reconciliation or saved builder prerequisites require operator action. Every other code uses the generic Problem display. Article invalidation comes only from observing `release.status === "success"`; never infer an article ID or mode.

- [ ] **Step 5: Verify green**

```bash
cd admin
npm test -- --run src/publishing/PublishingPage.test.tsx src/publishing/release-cache.test.ts src/editor/ArticleEditorPage.test.tsx src/articles/ArticleListPage.test.tsx
npm run typecheck
```

Expected: PASS for separate status domains, exact requests, polling, failures, same-Release/new-Job retry, safe builder snapshot, and absent unsupported controls.

- [ ] **Step 6: Commit**

```bash
git add admin/src/publishing admin/src/editor/ArticleEditorPage.tsx admin/src/articles/ArticleListPage.tsx admin/src/app/AppRouter.tsx admin/src/api/query-keys.ts
git commit -m "feat(admin): add release publishing controls"
```

### Task 12: Build Exact Site, SEO, Social, About, and ICP Settings

**Files:**
- Create: `admin/src/settings/settings-validation.ts`
- Create: `admin/src/settings/SiteSettingsPage.tsx`
- Create: `admin/src/settings/SiteSettingsPage.test.tsx`
- Modify: `admin/src/app/AppRouter.tsx`
- Modify: `admin/src/api/query-keys.ts`

**Interfaces:** Consumes `getSiteSettings`, `putSiteSettings`, `createRelease`, `syncReleaseCache`, `SiteSettingsView`, and `PutSiteSettingsRequest`. Produces `/settings/site`, optimistic save, and separate settings Release creation.

- [ ] **Step 1: Write failing exact-field tests**

Cover nullable `id` and `updatedAt`; lock zero virtual defaults; `siteName`, `authorName`, `authorBio`, `homeStatus`, `aboutMd`, ordered `socialLinks`, `seoDefaultTitle`, `seoDefaultDescription`, nullable `seoDefaultImageMediaId`, `filingName`, `filingNumber`; and read-only `filingUrl === "https://beian.miit.gov.cn/"`. Assert the PUT body omits `id`, `updatedAt`, and `filingUrl`.

Cover defaults `qiuxs`, `长安休息室`, and `浙ICP备17057726号-1`; field validation; dirty navigation; `409 settings_conflict` preserving local data; save success marked pending publication; and separate Release request `{mode:"publish_settings", articleId:null}`.

- [ ] **Step 2: Verify red**

Run: `cd admin && npm test -- --run src/settings/SiteSettingsPage.test.tsx`

Expected: FAIL because the site form is absent.

- [ ] **Step 3: Implement exact validation/request construction**

Require nonblank filing name/number. Enforce service rune limits: site and author names 100, author bio 1,000, home status 500, SEO title 100, SEO description 300, and each filing field 100. Preserve About Markdown, cap it at 2 MiB, and ensure the full encoded PUT JSON remains within 2 MiB. Permit at most 16 ordered social links, require canonical HTTPS URLs, and require case-insensitively unique nonblank labels.

```ts
const request: PutSiteSettingsRequest = {
  lockVersion: view.lockVersion,
  siteName,
  authorName,
  authorBio,
  homeStatus,
  aboutMd,
  socialLinks,
  seoDefaultTitle,
  seoDefaultDescription,
  seoDefaultImageMediaId,
  filingName,
  filingNumber,
};
```

- [ ] **Step 4: Implement save/publication separation**

Use an accessible form with ordered add/remove/reorder social controls, upload-selected default media ID, duplicate-submit blocking, and query cache replaced only by server response. Render `filingUrl` as fixed read-only link. After save announce “Saved settings — pending publication.” A separate confirmed action calls the generic Release request, awaits `syncReleaseCache(queryClient, result.release, "create")`, and navigates by validated Release ID. It does not special-case article caches by request mode; only a returned successful Release triggers the helper's broad article invalidation. Conflict offers copy of only PUT fields and confirmed reload.

- [ ] **Step 5: Verify green**

```bash
cd admin
npm test -- --run src/settings/SiteSettingsPage.test.tsx src/publishing/PublishingPage.test.tsx
npm run typecheck
```

Expected: PASS for every actual field, nullable/read-only values, exact PUT, settings conflict, and separate settings Release.

- [ ] **Step 6: Commit**

```bash
git add admin/src/settings/settings-validation.ts admin/src/settings/SiteSettingsPage.tsx admin/src/settings/SiteSettingsPage.test.tsx admin/src/app/AppRouter.tsx admin/src/api/query-keys.ts
git commit -m "feat(admin): add site settings"
```

### Task 13: Build Secret-Safe Jenkins Builder Configuration

**Files:**
- Create: `admin/src/settings/BuilderSettingsPage.tsx`
- Create: `admin/src/settings/BuilderSettingsPage.test.tsx`
- Modify: `admin/src/settings/settings-validation.ts`
- Modify: `admin/src/app/AppRouter.tsx`

**Interfaces:** Consumes `getBuilderConfig`, `putBuilderConfig`, `testBuilderConfig`, `BuilderConfigView`, and `PutBuilderConfigRequest`. Produces `/settings/builder`, editable GET-404 first-config state, conditional token preservation, and empty-body connection-test success.

- [ ] **Step 1: Write failing builder contract/security tests**

Cover returned `id`, `name`, `baseUrl`, `username`, `jobName`, `enabled`, and `tokenConfigured`; no lock version; token absence on reads; and exact `/api/admin/v1/builder` and `/builder/test` paths. A GET 404 renders an editable empty first-configuration form rather than the route error state. First save requires a token of 1–4096 Unicode code points; a configured view may omit a blank token to preserve the stored value. A nonblank token is sent once then removed from state/DOM, and never appears in Query cache, storage, URL, exceptions, or MSW diagnostics.

Trim name/username before validation and PUT; reject blank values, name over 100 runes, username over 255 runes, and any username containing `:`. Require a canonical HTTPS root origin of at most 2048 characters: lowercase ASCII DNS or canonical IPv4 host, no trailing dot, userinfo, path (including `/`), query, fragment, IPv6, default `:443`, zero-padded/zero/out-of-range port, uppercase scheme/host, or noncanonical IPv4. Job Name is 1–128 ASCII bytes, matches `^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`, and every slash-delimited segment is nonempty and neither `.` nor `..`. Cover connection test disabled for the 404 state or while the form differs from saved config, test 204 showing a local generic success announcement, and Problem-only test failure.

- [ ] **Step 2: Verify red**

Run: `cd admin && npm test -- --run src/settings/BuilderSettingsPage.test.tsx`

Expected: FAIL because the builder page is absent.

- [ ] **Step 3: Implement exact PUT construction**

```ts
const normalizedName = name.trim();
const normalizedUsername = username.trim();
if (!hasStoredConfig && [...token].length === 0) {
  throw new Error("API Token is required for the first builder configuration");
}
const request: PutBuilderConfigRequest = {
  name: normalizedName,
  baseUrl: normalizeBuilderUrl(baseUrl),
  username: normalizedUsername,
  jobName,
  enabled,
  ...(token === "" ? {} : { token }),
};
```

`normalizeBuilderUrl` returns the input only when it is already the canonical root origin described in Step 1; it does not silently rewrite a slash, case, host, or port. Validate Job Name by the exact regex and segment rules before PUT. Count token length by Unicode code points, retain its bytes verbatim, and reject explicit empty input on first save before calling the API. There is no optimistic-lock field. Validate returned ID and render `tokenConfigured` as “Stored token configured” without synthesizing token text. A 409 `builder_conflict` is a sanitized save conflict, not a stale form-version conflict.

- [ ] **Step 4: Implement save/test UX**

On GET 404 initialize empty name/base URL/username/token/job name with enabled false and announce “No builder configured”; do not cache a fabricated `BuilderConfigView`. Use a password input with `autocomplete="new-password"`; show “Token required for first save” in the 404 state and “Leave blank to keep the stored token” only for an existing view. On successful PUT replace cached `BuilderConfigView`, switch to existing state, and synchronously clear the token input. Enable connection test only for an enabled, unchanged saved view with `tokenConfigured === true`; call `testBuilderConfig()` and treat resolved `undefined` as success. Do not expect or display a result body/message.

- [ ] **Step 5: Verify green**

```bash
cd admin
npm test -- --run src/settings/BuilderSettingsPage.test.tsx src/api/admin-api.test.ts
npm run typecheck
```

Expected: PASS for token redaction/clearing, exact URL and request, no lock field, and empty 204 test success.

- [ ] **Step 6: Commit**

```bash
git add admin/src/settings/BuilderSettingsPage.tsx admin/src/settings/BuilderSettingsPage.test.tsx admin/src/settings/settings-validation.ts admin/src/app/AppRouter.tsx
git commit -m "feat(admin): add builder configuration"
```

### Task 14: Build Immediately Effective Hotlink Settings

**Files:**
- Create: `admin/src/settings/HotlinkSettingsPage.tsx`
- Create: `admin/src/settings/HotlinkSettingsPage.test.tsx`
- Modify: `admin/src/settings/settings-validation.ts`
- Modify: `admin/src/app/AppRouter.tsx`

**Interfaces:** Consumes `getHotlinkSettings`, `putHotlinkSettings`, `HotlinkSettingsView`, and `PutHotlinkSettingsRequest`. Produces `/settings/hotlink` and atomic full-list replacement.

- [ ] **Step 1: Write failing exact-entry tests**

Cover `{allowEmptyReferer, entries}`; defaults `qiuxs.com` and `blog-admin.qiuxs.com` enabled with empty Referer allowed; accept and trim outer whitespace in `" qiuxs.COM. "`; accept the single-label host `"localhost"`; lowercase and remove one trailing dot. Accept total length 253 and label length 63; reject total length 254, a 64-byte label, empty labels, non-ASCII, labels whose first/last byte is `-`, and characters outside ASCII letters/digits/hyphen. Reject schemes, paths, ports, wildcards, credentials, canonical/noncanonical IP literals, every input containing only digits and dots (including `"123"`), internal whitespace, and normalized duplicates. Cover enable/disable/delete by hostname, exact PUT body, no entry ID, no lock version, `409 settings_conflict` generic retention, and immediate-effective success without Release creation.

- [ ] **Step 2: Verify red**

Run: `cd admin && npm test -- --run src/settings/HotlinkSettingsPage.test.tsx`

Expected: FAIL because the hotlink page is absent.

- [ ] **Step 3: Implement normalization and exact request**

```ts
export function normalizeAllowedHostname(raw: string): string {
  let candidate = raw.trim();
  if (candidate.endsWith(".")) candidate = candidate.slice(0, -1);
  candidate = candidate.toLowerCase();
  if (
    candidate === "" || candidate.length > 253 || /[^\x00-\x7f]/.test(candidate) ||
    candidate.includes(":") || /^[0-9.]+$/.test(candidate)
  ) throw new Error("Enter an ASCII hostname, not an IP address");
  for (const label of candidate.split(".")) {
    if (
      label.length < 1 || label.length > 63 || label.startsWith("-") ||
      label.endsWith("-") || !/^[a-z0-9-]+$/.test(label)
    ) throw new Error("Each hostname label must use letters, digits, or interior hyphens");
  }
  return candidate;
}

const request: PutHotlinkSettingsRequest = { allowEmptyReferer, entries };
```

- [ ] **Step 4: Implement responsive editor and immediate save**

Use a table wide and labelled cards narrow. Each entry has enabled checkbox and host-named Delete confirmation. PUT the entire normalized array. Replace cache from response and announce “Image hotlink rules are active now”; never offer a site Release action.

- [ ] **Step 5: Verify green**

```bash
cd admin
npm test -- --run src/settings/HotlinkSettingsPage.test.tsx
npm run typecheck
```

Expected: PASS for defaults, normalization, entries without IDs/lock, conflict retention, responsive controls, and immediate effect.

- [ ] **Step 6: Commit**

```bash
git add admin/src/settings/HotlinkSettingsPage.tsx admin/src/settings/HotlinkSettingsPage.test.tsx admin/src/settings/settings-validation.ts admin/src/app/AppRouter.tsx
git commit -m "feat(admin): add hotlink settings"
```

### Task 15: Prove Browser Flows with a Strict Contract Mock

> **Owner decision:** cancel this automated UI/Playwright task. Do not create `admin/e2e`, install Playwright, or add browser acceptance tests. The owner will manually inspect the completed Admin UI; only deterministic pure logic and API contract tests remain in the automated gate.

**Files:**
- Create: `admin/playwright.config.ts`
- Create: `admin/e2e/support/mock-admin-api.ts`
- Create: `admin/e2e/mock-contract.spec.ts`
- Create: `admin/e2e/auth-editor.spec.ts`
- Create: `admin/e2e/conflict-upload-release.spec.ts`
- Create: `admin/e2e/settings-responsive.spec.ts`
- Modify: `admin/package.json`
- Modify: `admin/package-lock.json`

**Interfaces:** Consumes the built SPA and generated aliases. Produces isolated Chromium acceptance with no real backend/network.

- [ ] **Step 1: Write the failing mock-contract spec**

In `mock-contract.spec.ts`, install routes before navigation, open the local preview origin, and issue table-driven browser-context `fetch` calls with `page.evaluate` for every Stage Entry row. Assert all 29 exact method/path/status combinations, no body on bodyless operations, exact generated response shapes, strict request validation, empty 204 bodies, 202 Release results, and rejection of an unknown same-origin path. The import below is intentionally red first:

```ts
import { installMockAdminApi, mockAdminState } from "./support/mock-admin-api";

test("implements exactly the browser Admin contract", async ({ page }) => {
  const controller = await installMockAdminApi(page, mockAdminState());
  expect(controller.registeredOperationIds).toHaveLength(29);
  expect(new Set(controller.registeredOperationIds).size).toBe(29);
});
```

- [ ] **Step 2: Verify red**

Run: `cd admin && npm run test:e2e -- e2e/mock-contract.spec.ts`

Expected: FAIL because the Playwright config and mock module are absent.

- [ ] **Step 3: Implement the strict stateful mock**

```ts
export interface MockAdminState {
  authenticated: boolean;
  article: ArticleDetail;
  preview: PreviewView;
  versions: RevisionList;
  tags: TagList;
  media: MediaView[];
  siteSettings: SiteSettingsView;
  builderConfig: BuilderConfigView;
  hotlinkSettings: HotlinkSettingsView;
  releases: ReleaseList;
}

export async function installMockAdminApi(
  page: Page,
  state: MockAdminState,
): Promise<MockAdminController>;
```

Register only the 29 Stage Entry operations: session/me; article collection/detail/draft/preview/version/restore/trash/untrash; tag collection/rename; media policy/register; site/hotlink; builder/config test; Release collection/detail/retry. Each handler validates exact query/path/body, returns the documented success status/content type/schema, and uses generated `satisfies` fixtures. Void operations return empty 204; Release creation/retry return 202; no unregistered same-origin request succeeds.

Mock `https://gfs.test/v1/upload` separately. Verify multipart policy fields and file bytes; return `{ code: 0, data: { val: 41, objectInfo: { fileSize: "8192", format: "png", imageWidth: "640", imageHeight: "480" } } }`. Expose controls for next draft save `409 revision_conflict`, next settings save `409 settings_conflict`, next Release create `409 release_conflict` or `412 precondition_failed`, and next dependency request 503.

- [ ] **Step 4: Write the failing auth/editor/preview flow**

Start anonymous at an editor URL, login, return to it, paste the shared whole-GFM fixture, wait for two-second save, reload, verify content, switch source/visual, create a tag and version, open server-backed preview, verify highlighted code/public image origin, and logout.

- [ ] **Step 5: Write the failing conflict/upload/Release flow**

Paste an in-memory PNG, assert bodyless policy precedes GFS and exact media registration, observe returned stable URL, force revision conflict, copy local Markdown, reload server draft, save, create article Release, observe latest job pending/queued/building/deploying/success via Release GET polling, then make a failed Release retry and assert same Release/new Job with preserved history.

- [ ] **Step 6: Write the failing responsive-settings flow**

At 390-by-844, keyboard-operate navigation; update every actual site field, confirm fixed filing URL, save then separately create settings Release; submit builder token and prove it disappears, test saved builder with empty 204; update `entries` hotlink list without Release; run axe on login/editor/publishing/settings pages.

- [ ] **Step 7: Configure and verify the full browser gate**

Configure build+preview server at `127.0.0.1:4173`, no reuse, Chromium only, trace first retry, screenshot failure, 30-second timeout, and catch-all abort for hosts other than preview, `gfs.test`, and mocked public images.

```bash
cd admin
npm run build
npx playwright install chromium
npm run test:e2e -- e2e/mock-contract.spec.ts
npm run test:e2e -- e2e/auth-editor.spec.ts
npm run test:e2e -- e2e/conflict-upload-release.spec.ts
npm run test:e2e -- e2e/settings-responsive.spec.ts
```

Expected: all four specs PASS with no external connection; the contract spec reports exactly 29 unique registered operation IDs.

The explicit `npx playwright install chromium` is an idempotent part of this gate. Do not omit it or depend on a Chromium binary left in a shared cache by another workspace.

- [ ] **Step 8: Commit**

```bash
git add admin/playwright.config.ts admin/e2e admin/package.json admin/package-lock.json
git commit -m "test(admin): cover browser management flows"
```

### Task 16: Finalize Documentation and the Host-Build Artifact Gate

**Files:**
- Create: `admin/Makefile`
- Create: `admin/README.md`
- Create: `admin/scripts/verify-dist.test.mjs`
- Modify: `admin/scripts/verify-dist.mjs`
- Modify: `admin/package.json`
- Modify: `README.md`

**Interfaces:** Consumes all Admin scripts. Produces exact Make targets, verified static output, route/API documentation, and Stage 6 handoff boundaries.

- [ ] **Step 1: Write failing artifact assertions**

Write `verify-dist.test.mjs` fixtures that make the verifier reject missing index, unhashed JS/CSS, source maps, absolute local paths, any one of these exact protected identifiers, output above 2 MiB per file, and HTML references to missing assets:

```js
const protectedIdentifiers = Object.freeze([
  "BLOG_ADMIN_PASSWORD",
  "BLOG_REDIS_PASSWORD",
  "BLOG_GFS_APP_SECRET",
  "BLOG_GFS_PUBLIC_READ_SECRET",
  "BLOG_BUNDLE_TOKEN",
  "BLOG_CALLBACK_HMAC_KEY",
  "BLOG_BUILDER_MASTER_KEY",
]);
```

Test every identifier individually and one valid fixture with hashed JS/CSS. Scan for these exact case-sensitive strings only; do not generically reject the words `token`, `password`, or `secret`, because legitimate UI copy and generated field names contain them. Bundle only plaintext, Bash, JSON, YAML, Go, JavaScript, TypeScript, JSX, TSX, SQL, HTML, CSS, and Markdown grammars; other fences render plaintext.

- [ ] **Step 2: Verify red**

Run: `cd admin && node --test scripts/require-node.test.mjs scripts/verify-dist.test.mjs`

Expected: FAIL because the reusable verifier checks required by the tests are absent.

- [ ] **Step 3: Implement verifier checks and add exact Make targets**

Export the reusable verifier entry used by `verify-dist.test.mjs`, implement each red assertion from Step 1, then keep the CLI wrapper for `npm run verify:dist`. Add these Make targets:

```make
.PHONY: version-check install generate test build

version-check:
	@test "$$(node --version)" = "v20.19.4" || (echo "Node 20.19.4 required, got $$(node --version)" && exit 1)

install: version-check
	npm ci

generate: version-check
	npm run generate:api

test: version-check
	npm run typecheck
	npm run test:run

build: version-check test
	npm run generate:api
	git diff --exit-code -- src/api/generated/admin.ts
	npm run build
	npm run verify:dist
```

- [ ] **Step 4: Document the real boundary and Stage 6 handoff**

Document Node 20.19.4, `npm ci`, generation, same-origin proxy/cookie, tests, all routes, actual Admin operation groups, direct GFS boundary, fixed preview image origin, and complete `dist/`. State that Stage 6 deploys the whole directory to a new `/web/deploy/blog-admin/releases/<revision>`, validates index/assets, configures history fallback, and atomically switches `current`; add no Jenkins/Nginx files here.

- [ ] **Step 5: Run the clean Stage 4 gate**

```bash
cd admin
test "$(node --version)" = "v20.19.4"
rm -rf node_modules dist coverage playwright-report test-results
npm ci
npm run generate:api
git diff --exit-code -- src/api/generated/admin.ts
npm run typecheck
npm run test:run
npm run build
npm run verify:dist
git diff --check
```

Expected: PASS with no real service dependency or application request to the external network; generated types remain clean and `dist/` is a reproducible ignored artifact. UI acceptance is manual and is not part of this command.

- [ ] **Step 6: Perform manual responsive/accessibility smoke**

Inspect login, article list, editor visual/source, preview, versions, publishing, and settings at 1440-by-900 and 390-by-844. Complete flows by keyboard; verify focus, announcements, local input retention, no page-wide overflow, and cold-engineering hierarchy.

- [ ] **Step 7: Commit**

```bash
git add admin/Makefile admin/README.md admin/scripts/verify-dist.mjs admin/scripts/verify-dist.test.mjs admin/package.json README.md
git commit -m "docs(admin): define build and deployment artifact"
```

- [ ] **Step 8: Confirm clean implementation branch**

Run: `git status --short`

Expected: no output; ignored dependencies, build, coverage, and Playwright artifacts do not appear.

---

## Stage 4 Completion Gate

Stage 4 is complete only when all sixteen task commits are independently reviewable and Task 16's clean gate passes on Node.js exactly 20.19.4. Browser acceptance proves login, whole-GFM paste, two-second autosave, revision-conflict recovery, direct upload, source/preview, version create/restore, Release create/retry/polling, and all settings without a real backend. The committed output is source, tests, shared Markdown contracts, documentation, and lockfile; Stage 6 exclusively owns Jenkins definitions, rsync, deployment directories, Nginx proxy/history fallback, atomic symlink switching, and deployed-environment smoke tests.

Before implementation handoff, the plan author must mechanically compare the Stage Entry operation IDs against `contracts/openapi/admin-v1.yaml`, scan for placeholder language, inspect every generated alias/method/task name for type consistency, and run `git diff --check`. Any mismatch is fixed in this plan before Task 1 begins.
