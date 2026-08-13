# React Admin Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the protected React Admin SPA that lets the single administrator manage articles, edit and preview GFM Markdown, upload images directly through GFS, manage versions and publishing, and edit site, builder, and hotlink settings.

**Architecture:** `admin/` is a same-origin Vite SPA: Nginx will serve its static files and proxy `/api/*` to the Go service in Stage 6, so the browser client uses the existing HttpOnly Session cookie and never stores a token. OpenAPI generates TypeScript `paths` and `components`; a small `openapi-fetch` adapter exposes stable feature methods, TanStack Query owns server state, and editor-local state owns unsaved Markdown plus the two-second optimistic-lock autosave state machine. Milkdown is the default canvas, source and preview are explicit modes/routes, and tests replace the network with MSW or Playwright route handlers rather than starting the Go service.

**Tech Stack:** Node.js 20.19.4, npm lockfile, React, TypeScript strict mode, Vite, React Router, TanStack Query, `openapi-typescript`, `openapi-fetch`, Milkdown Kit with CommonMark and the approved GFM plugins, unified/remark/rehype with Shiki for protected preview, Vitest, React Testing Library, MSW, jest-axe, and Playwright with `@axe-core/playwright`.

## Global Constraints

- Run every Admin install, generation, test, and build command on the Jenkins host or developer host with `node --version` exactly `v20.19.4`; do not add an Admin Docker build.
- Commit `admin/package-lock.json`; clean verification uses `npm ci`, never an unfrozen install.
- Stage 4 consumes the Stage 2 and Stage 3 Admin OpenAPI contract. It does not add Go handlers, SQL, Release orchestration, Jenkinsfiles, Nginx configuration, or deployment scripts.
- `contracts/openapi/admin-v1.yaml` remains the API source of truth. Never hand-edit `admin/src/api/generated/admin.ts`; regenerate it and fail the gate on a diff.
- All API calls are same-origin, use `credentials: "include"`, and rely on the browser-generated `Origin` header for unsafe methods. Never add `Authorization`, persist a Session token, or put a password or Jenkins token in logs, URLs, local storage, or error text.
- Treat every OpenAPI `int64` ID as a JavaScript `number` only after `Number.isSafeInteger(value) && value > 0`; reject unsafe values before rendering links or sending mutations.
- Milkdown is the default editor. The stored `contentMd` string is the content truth; visual mode, source mode, autosave, version creation, image insertion, and preview all read or update that same value.
- Support only the approved GFM subset: headings, paragraphs, emphasis, strikethrough, ordered/unordered/task lists, quotes, links, images, tables, inline code, and fenced code. Raw HTML, Mermaid, formulas, footnotes, MDX, and custom directives are rejected or rendered as text.
- Autosave waits 2,000 ms after the latest edit, sends the current `lockVersion`, serializes writes per article, and never overwrites a `409 draft_conflict`. Failed or conflicting content remains in memory and navigation warns until the user retries, copies, or deliberately reloads the server draft.
- Image uploads use the service-issued, approximately 60-second GFS policy, send the file directly to the returned GFS URL, register returned metadata with the Admin API, and insert only `/img/proxy/{publicKey}`. Never save a `blob:`, GFS numeric ID, OSS URL, policy field, or signed URL in Markdown.
- Preview is an authenticated SPA route, never creates a Release, never triggers Jenkins, and rewrites `/img/proxy/*` to `https://qiuxs.com/img/proxy/*` before rendering.
- Use the cold-engineering visual system: charcoal surfaces, cold-white copy, electric-blue focus/action color, restrained grid and borders, no hacker-green theme, character rain, or fault animation.
- Meet WCAG 2.2 AA for contrast and semantics; all controls have accessible names, keyboard operation, visible focus, error association, status announcements, and at least 44-by-44 CSS-pixel touch targets on narrow screens.
- Every route has explicit initial loading, empty, retryable failure, authorization loss, and successful-content states. Mutations disable duplicate submission and retain a visible sanitized Problem summary plus `requestId`.
- Unit/component tests use Vitest, React Testing Library, MSW, and fake timers. Browser tests use Playwright route fulfillment and an external fake GFS origin; no automated Admin test starts containers or connects to a real service, MySQL, Redis, Jenkins, GFS, or OSS.
- Production output is a static `admin/dist/` containing `index.html`, hashed assets, and no source maps or secrets. Stage 6 will rsync that complete directory into `/web/deploy/blog-admin/releases/<revision>` and configure SPA fallback plus atomic `current` switching.

---

## Stage Entry Contract

Do not begin Task 2 until Stages 2 and 3 have extended `contracts/openapi/admin-v1.yaml` and their service tests pass. Task 2 deliberately compiles against these exact operation IDs and response schemas:

| Method and path | operationId | Success payload |
| --- | --- | --- |
| `POST /api/admin/v1/session` | `loginAdmin` | `AdminView` |
| `DELETE /api/admin/v1/session` | `logoutAdmin` | `204` |
| `GET /api/admin/v1/me` | `getCurrentAdmin` | `AdminView` |
| `GET /api/admin/v1/articles` | `listArticles` | `ArticleListPage` |
| `POST /api/admin/v1/articles` | `createArticle` | `ArticleEditorView` |
| `GET /api/admin/v1/articles/{articleId}` | `getArticleEditor` | `ArticleEditorView` |
| `PUT /api/admin/v1/articles/{articleId}/draft` | `saveArticleDraft` | `DraftRevision` |
| `POST /api/admin/v1/articles/{articleId}/trash` | `trashArticle` | `ArticleSummary` |
| `POST /api/admin/v1/articles/{articleId}/restore` | `restoreArticle` | `ArticleSummary` |
| `GET /api/admin/v1/tags` | `listTags` | `TagView[]` |
| `GET /api/admin/v1/articles/{articleId}/versions` | `listArticleVersions` | `ArticleVersionPage` |
| `POST /api/admin/v1/articles/{articleId}/versions` | `createArticleVersion` | `ArticleEditorView` |
| `POST /api/admin/v1/articles/{articleId}/versions/{revisionId}/restore` | `restoreArticleVersion` | `ArticleEditorView` |
| `POST /api/admin/v1/media/upload-policy` | `createMediaUploadPolicy` | `MediaUploadPolicy` |
| `POST /api/admin/v1/media` | `registerMedia` | `MediaView` |
| `POST /api/admin/v1/articles/{articleId}/publish` | `publishArticle` | `PublishJobView` |
| `POST /api/admin/v1/articles/{articleId}/unpublish` | `unpublishArticle` | `PublishJobView` |
| `GET /api/admin/v1/publish-jobs` | `listPublishJobs` | `PublishJobPage` |
| `GET /api/admin/v1/publish-jobs/{publishJobId}` | `getPublishJob` | `PublishJobView` |
| `POST /api/admin/v1/publish-jobs/{publishJobId}/retry` | `retryPublishJob` | `PublishJobView` |
| `POST /api/admin/v1/publish-jobs/{publishJobId}/terminate` | `terminatePublishJob` | `PublishJobView` |
| `POST /api/admin/v1/publishing/site` | `publishSite` | `PublishJobView` |
| `GET /api/admin/v1/settings/site` | `getSiteSettings` | `SiteSettingsView` |
| `PUT /api/admin/v1/settings/site` | `updateSiteSettings` | `SiteSettingsView` |
| `GET /api/admin/v1/settings/builder` | `getBuilderSettings` | `BuilderSettingsView` |
| `PUT /api/admin/v1/settings/builder` | `updateBuilderSettings` | `BuilderSettingsView` |
| `POST /api/admin/v1/settings/builder/test` | `testBuilderConnection` | `BuilderConnectionResult` |
| `GET /api/admin/v1/settings/hotlink` | `getHotlinkSettings` | `HotlinkSettingsView` |
| `PUT /api/admin/v1/settings/hotlink` | `updateHotlinkSettings` | `HotlinkSettingsView` |

The generated component schemas must expose these fields so the Admin never invents data absent from the service contract:

```ts
type EntityId = number;
type PublishJobStatus =
  | "pending"
  | "queued"
  | "building"
  | "deploying"
  | "success"
  | "failed";

interface DraftRevision {
  id: EntityId;
  articleId: EntityId;
  revisionNo: number;
  title: string;
  summary: string;
  coverMediaId: EntityId | null;
  coverMedia: MediaView | null;
  contentMd: string;
  tags: TagView[];
  lockVersion: number;
  updatedAt: string;
}

interface SaveArticleDraftRequest {
  lockVersion: number;
  title: string;
  summary: string;
  coverMediaId: EntityId | null;
  contentMd: string;
  tagNames: string[];
}

interface MediaUploadPolicy {
  uploadUrl: string;
  fields: Record<string, string>;
  expiresAt: string;
  maxFileSize: number;
  allowedMimeTypes: string[];
}

interface BuilderSettingsView {
  name: string;
  baseUrl: string;
  username: string;
  jobName: string;
  enabled: boolean;
  hasToken: boolean;
  lockVersion: number;
}

interface Problem {
  type: string;
  title: string;
  status: number;
  code: string;
  requestId: string;
}

interface LoginRequest { username: string; password: string }
interface AdminView { id: EntityId; username: string }
interface TagView { id: EntityId; name: string; slug: string }

interface ArticleListQuery {
  state: "active" | "trashed" | "all";
  page: number;
  pageSize: 20;
}

interface ArticleSummary {
  id: EntityId;
  slug: string;
  title: string;
  state: "active" | "trashed";
  draftUpdatedAt: string;
  publishedRevisionId: EntityId | null;
  publicationState: "unpublished" | "published" | "publish_pending" | "unpublish_pending";
  lastPublishStatus: PublishJobStatus | null;
}

interface ArticleListPage {
  items: ArticleSummary[];
  page: number;
  pageSize: 20;
  total: number;
}

interface CreateArticleRequest { title: string }

interface ArticleEditorView {
  article: ArticleSummary;
  draft: DraftRevision;
}

interface ArticleVersionSummary {
  id: EntityId;
  revisionNo: number;
  reason: "manual_version" | "publish_snapshot";
  title: string;
  summary: string;
  tags: TagView[];
  createdAt: string;
}

interface ArticleVersionPage {
  items: ArticleVersionSummary[];
  page: number;
  pageSize: number;
  total: number;
}

interface MediaUploadPolicyRequest {
  originalName: string;
  mimeType: string;
  fileSize: number;
}

interface RegisterMediaRequest {
  fileId: string;
  originalName: string;
  mimeType: string;
  fileSize: number;
  width: number;
  height: number;
}

interface MediaView {
  id: EntityId;
  publicKey: string;
  originalName: string;
  mimeType: string;
  fileSize: number;
  width: number;
  height: number;
}

interface PublishJobView {
  id: EntityId;
  releaseId: EntityId;
  operation: "publish_article" | "unpublish_article" | "publish_site";
  articleId: EntityId | null;
  status: PublishJobStatus;
  stage: string;
  buildNumber: number | null;
  errorSummary: string | null;
  createdAt: string;
  updatedAt: string;
}

interface PublishJobPage {
  items: PublishJobView[];
  page: number;
  pageSize: number;
  total: number;
}

interface SocialLink { label: string; url: string }

interface SiteSettingsView {
  siteName: string;
  authorIntroduction: string;
  homeStatus: string;
  aboutMd: string;
  socialLinks: SocialLink[];
  seoTitle: string;
  seoDescription: string;
  defaultOgMediaId: EntityId | null;
  filingName: string;
  filingNumber: string;
  lockVersion: number;
  updatedAt: string;
}

interface UpdateSiteSettingsRequest {
  siteName: string;
  authorIntroduction: string;
  homeStatus: string;
  aboutMd: string;
  socialLinks: SocialLink[];
  seoTitle: string;
  seoDescription: string;
  defaultOgMediaId: EntityId | null;
  filingName: string;
  filingNumber: string;
  lockVersion: number;
}

interface UpdateBuilderSettingsRequest {
  name: string;
  baseUrl: string;
  username: string;
  token?: string;
  jobName: string;
  enabled: boolean;
  lockVersion: number;
}

interface BuilderConnectionResult { success: boolean; message: string }

interface RefererHostView {
  id: EntityId;
  hostname: string;
  enabled: boolean;
}

interface HotlinkSettingsView {
  allowEmptyReferer: boolean;
  hosts: RefererHostView[];
  lockVersion: number;
}

interface UpdateHotlinkSettingsRequest {
  allowEmptyReferer: boolean;
  hosts: Array<{ id: EntityId | null; hostname: string; enabled: boolean }>;
  lockVersion: number;
}
```

These declarations describe required OpenAPI schemas; Admin source aliases them from `components["schemas"]` rather than maintaining duplicate handwritten wire types. `Problem.code` uses `unauthenticated` for expired sessions, `draft_conflict` for stale `lockVersion`, `publish_in_progress` for the global publishing lock, and stable domain-specific codes for other failures. Builder reads never contain a token field; `UpdateBuilderSettingsRequest.token` is optional and omission means preserve the encrypted value.

## Exact File Map

```text
admin/
├── .nvmrc
├── Makefile
├── README.md
├── index.html
├── package.json
├── package-lock.json
├── playwright.config.ts
├── tsconfig.json
├── vite.config.ts
├── vitest.config.ts
├── scripts/
│   ├── require-node.mjs
│   ├── require-node.test.mjs
│   └── verify-dist.mjs
├── src/
│   ├── main.tsx
│   ├── app/
│   │   ├── AppProviders.tsx
│   │   ├── AppRouter.tsx
│   │   └── RouteErrorPage.tsx
│   ├── api/
│   │   ├── admin-api.ts
│   │   ├── admin-api.test.ts
│   │   ├── generated/admin.ts
│   │   ├── ids.ts
│   │   ├── problem.ts
│   │   └── query-keys.ts
│   ├── auth/
│   │   ├── AuthProvider.tsx
│   │   ├── RequireSession.tsx
│   │   ├── LoginPage.tsx
│   │   └── auth.test.tsx
│   ├── components/
│   │   ├── AsyncPage.tsx
│   │   ├── ConfirmDialog.tsx
│   │   ├── FormField.tsx
│   │   ├── ProblemNotice.tsx
│   │   ├── SaveIndicator.tsx
│   │   └── StatusBadge.tsx
│   ├── layout/
│   │   ├── AppShell.tsx
│   │   └── AppShell.test.tsx
│   ├── articles/
│   │   ├── ArticleListPage.tsx
│   │   ├── ArticleListPage.test.tsx
│   │   └── article-actions.ts
│   ├── editor/
│   │   ├── ArticleEditorPage.tsx
│   │   ├── ArticleEditorPage.test.tsx
│   │   ├── ConflictDialog.tsx
│   │   ├── MarkdownEditor.tsx
│   │   ├── editor-document.ts
│   │   ├── editor-document.test.ts
│   │   ├── milkdown-adapter.ts
│   │   ├── useAutosave.ts
│   │   └── useAutosave.test.tsx
│   ├── media/
│   │   ├── image-upload.ts
│   │   ├── image-upload.test.ts
│   │   └── useEditorImageUpload.ts
│   ├── preview/
│   │   ├── ArticlePreviewPage.tsx
│   │   ├── ArticlePreviewPage.test.tsx
│   │   ├── render-markdown.ts
│   │   └── render-markdown.test.ts
│   ├── versions/
│   │   ├── ArticleVersionsPage.tsx
│   │   └── ArticleVersionsPage.test.tsx
│   ├── publishing/
│   │   ├── PublishingPage.tsx
│   │   ├── PublishingPage.test.tsx
│   │   └── publish-status.ts
│   ├── settings/
│   │   ├── SiteSettingsPage.tsx
│   │   ├── SiteSettingsPage.test.tsx
│   │   ├── BuilderSettingsPage.tsx
│   │   ├── BuilderSettingsPage.test.tsx
│   │   ├── HotlinkSettingsPage.tsx
│   │   ├── HotlinkSettingsPage.test.tsx
│   │   └── settings-validation.ts
│   ├── styles/
│   │   ├── base.css
│   │   ├── components.css
│   │   ├── editor.css
│   │   └── tokens.css
│   └── test/
│       ├── fixtures.ts
│       ├── handlers.ts
│       ├── render.tsx
│       ├── server.ts
│       └── setup.ts
└── e2e/
    ├── auth-editor.spec.ts
    ├── conflict-upload-publish.spec.ts
    ├── settings-responsive.spec.ts
    └── support/mock-admin-api.ts
contracts/markdown/
├── article-content.css
└── fixtures/full-gfm.md
```

Also modify root `.gitignore` to ignore `admin/node_modules/`, `admin/dist/`, `admin/coverage/`, `admin/playwright-report/`, and `admin/test-results/`; modify root `README.md` to link `admin/README.md`. Do not modify `service/`, `site/`, or `deploy/` in this stage.

## Shared Frontend Interfaces

These signatures are stable across tasks; keep feature components dependent on `AdminApi`, not on raw `openapi-fetch` calls:

```ts
export interface AdminApi {
  login(input: LoginRequest, signal?: AbortSignal): Promise<AdminView>;
  logout(signal?: AbortSignal): Promise<void>;
  currentAdmin(signal?: AbortSignal): Promise<AdminView>;
  listArticles(query: ArticleListQuery, signal?: AbortSignal): Promise<ArticleListPage>;
  createArticle(input: CreateArticleRequest): Promise<ArticleEditorView>;
  getArticleEditor(articleId: EntityId, signal?: AbortSignal): Promise<ArticleEditorView>;
  saveArticleDraft(articleId: EntityId, input: SaveArticleDraftRequest): Promise<DraftRevision>;
  trashArticle(articleId: EntityId): Promise<ArticleSummary>;
  restoreArticle(articleId: EntityId): Promise<ArticleSummary>;
  listTags(query: string, signal?: AbortSignal): Promise<TagView[]>;
  listArticleVersions(articleId: EntityId, signal?: AbortSignal): Promise<ArticleVersionPage>;
  createArticleVersion(articleId: EntityId): Promise<ArticleEditorView>;
  restoreArticleVersion(articleId: EntityId, revisionId: EntityId): Promise<ArticleEditorView>;
  createMediaUploadPolicy(input: MediaUploadPolicyRequest): Promise<MediaUploadPolicy>;
  registerMedia(input: RegisterMediaRequest): Promise<MediaView>;
  publishArticle(articleId: EntityId): Promise<PublishJobView>;
  unpublishArticle(articleId: EntityId): Promise<PublishJobView>;
  listPublishJobs(signal?: AbortSignal): Promise<PublishJobPage>;
  getPublishJob(publishJobId: EntityId, signal?: AbortSignal): Promise<PublishJobView>;
  retryPublishJob(publishJobId: EntityId): Promise<PublishJobView>;
  terminatePublishJob(publishJobId: EntityId): Promise<PublishJobView>;
  publishSite(): Promise<PublishJobView>;
  getSiteSettings(signal?: AbortSignal): Promise<SiteSettingsView>;
  updateSiteSettings(input: UpdateSiteSettingsRequest): Promise<SiteSettingsView>;
  getBuilderSettings(signal?: AbortSignal): Promise<BuilderSettingsView>;
  updateBuilderSettings(input: UpdateBuilderSettingsRequest): Promise<BuilderSettingsView>;
  testBuilderConnection(): Promise<BuilderConnectionResult>;
  getHotlinkSettings(signal?: AbortSignal): Promise<HotlinkSettingsView>;
  updateHotlinkSettings(input: UpdateHotlinkSettingsRequest): Promise<HotlinkSettingsView>;
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

export type SaveState =
  | { kind: "saved"; savedAt: Date; lockVersion: number }
  | { kind: "dirty"; lockVersion: number }
  | { kind: "saving"; lockVersion: number }
  | { kind: "failed"; lockVersion: number; problem: ApiProblem }
  | { kind: "conflict"; lockVersion: number; local: EditorDocument };
```

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

**Interfaces:**
- Consumes: repository layout and exact Node requirement from the approved design.
- Produces: `npm run check:node`, `npm run dev`, `npm run typecheck`, `npm test`, `npm run build`, `npm run verify:dist`, and static `admin/dist/`.

- [ ] **Step 1: Write the failing exact-version test**

```js
// admin/scripts/require-node.test.mjs
import test from "node:test";
import assert from "node:assert/strict";
import { assertNodeVersion } from "./require-node.mjs";

test("accepts only Node 20.19.4", () => {
  assert.doesNotThrow(() => assertNodeVersion("v20.19.4"));
  assert.throws(() => assertNodeVersion("v20.19.3"), /Node 20\.19\.4 required/);
  assert.throws(() => assertNodeVersion("v22.20.0"), /Node 20\.19\.4 required/);
});
```

- [ ] **Step 2: Run the test and observe the missing module**

Run: `cd admin && node --test scripts/require-node.test.mjs`

Expected: FAIL because `require-node.mjs` does not exist.

- [ ] **Step 3: Add the exact version guard and package scripts**

```js
// admin/scripts/require-node.mjs
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

export function assertNodeVersion(actual) {
  if (actual !== "v20.19.4") {
    throw new Error(`Node 20.19.4 required; received ${actual}`);
  }
}

if (process.argv[1] && fileURLToPath(import.meta.url) === resolve(process.argv[1])) {
  assertNodeVersion(process.version);
}
```

Set `.nvmrc` to `20.19.4`. Set `package.json` to ESM, `private: true`, `engines.node: "20.19.4"`, and scripts with this exact pipeline:

```json
{
  "scripts": {
    "check:node": "node scripts/require-node.mjs",
    "dev": "npm run check:node && vite",
    "preview": "vite preview",
    "generate:api": "openapi-typescript ../contracts/openapi/admin-v1.yaml -o src/api/generated/admin.ts",
    "typecheck": "tsc --noEmit",
    "test": "vitest",
    "test:run": "vitest run",
    "test:e2e": "playwright test",
    "build": "npm run check:node && npm run typecheck && vite build",
    "verify:dist": "node scripts/verify-dist.mjs"
  }
}
```

Install and lock the dependencies with these commands, then configure TypeScript with `strict`, `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`, and `moduleResolution: "Bundler"`:

```bash
cd admin
npm install react react-dom react-router-dom @tanstack/react-query openapi-fetch @milkdown/kit @milkdown/react unified remark-parse remark-gfm remark-rehype rehype-sanitize rehype-stringify @shikijs/rehype github-slugger
npm install --save-dev typescript vite @vitejs/plugin-react openapi-typescript vitest jsdom @testing-library/react @testing-library/user-event @testing-library/jest-dom msw jest-axe @types/react @types/react-dom @types/node @playwright/test @axe-core/playwright
```

Commit the exact resolved dependency graph in `package-lock.json`.

- [ ] **Step 4: Add the smallest render and static-output verifier**

Render `<div id="root">` from `index.html`, mount a temporary `Admin loading…` status from `src/main.tsx`, disable production source maps, use base `/`, and configure the Vite development proxy for `/api` to `http://127.0.0.1:8080` without changing the Host or Origin headers.

`verify-dist.mjs` must fail unless `dist/index.html` exists, at least one `dist/assets/*-[hash].js` and CSS asset exists, no `.map` file exists, and recursively read output contains none of these names: `BLOG_ADMIN_PASSWORD`, `BLOG_REDIS_PASSWORD`, `JENKINS_TOKEN`, `GFS_SECRET`, `BUILD_TOKEN`.

- [ ] **Step 5: Verify the bootstrap**

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

Expected: every command PASS; `dist/` contains only static output and remains ignored by Git.

- [ ] **Step 6: Commit**

```bash
git add .gitignore admin
git commit -m "chore(admin): bootstrap react workspace"
```

### Task 2: Generate the OpenAPI Types and Build the Sanitized API Boundary

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

**Interfaces:**
- Consumes: every operation and schema in the Stage Entry Contract.
- Produces: `createAdminApi(options?: AdminApiOptions): AdminApi`, `ApiProblem`, `requireEntityId(value: unknown, field: string): EntityId`, stable `queryKeys`, and reusable MSW fixtures.

- [ ] **Step 1: Generate the contract and write failing transport tests**

Run: `cd admin && npm run generate:api`

Then test that `currentAdmin()` sends a same-origin GET with `credentials: "include"`, a `401` Problem becomes `ApiProblem`, an invalid `id: 9007199254740992` is rejected, and neither a submitted password nor a builder token appears in thrown errors.

```ts
it("maps Problems without exposing request bodies", async () => {
  server.use(
    http.post("/api/admin/v1/session", () =>
      HttpResponse.json(problem("invalid_credentials", 401, "req-login"), {
        status: 401,
        headers: { "Content-Type": "application/problem+json" },
      }),
    ),
  );

  const api = createAdminApi();
  const rejected = await api.login({ username: "qiuxs", password: "password-secret" })
    .catch((error: unknown) => error);
  expect(rejected).toMatchObject({
    status: 401,
    code: "invalid_credentials",
    requestId: "req-login",
  });
  expect((rejected as Error).message).not.toContain("password-secret");
});
```

- [ ] **Step 2: Run the focused test and observe missing APIs**

Run: `cd admin && npm test -- --run src/api/admin-api.test.ts`

Expected: FAIL because `createAdminApi`, `ApiProblem`, fixtures, and ID guards are absent.

- [ ] **Step 3: Implement the generated-client adapter**

```ts
export function createAdminApi(options: AdminApiOptions = {}): AdminApi {
  const client = createClient<paths>({
    baseUrl: window.location.origin,
    credentials: "include",
    fetch: options.fetch ?? globalThis.fetch,
    headers: { Accept: "application/json, application/problem+json" },
  });

  const currentAdmin: AdminApi["currentAdmin"] = async (signal) => unwrap(
    await client.GET("/api/admin/v1/me", { signal }),
    options.onUnauthenticated,
  );
  const login: AdminApi["login"] = async (body, signal) => unwrap(
    await client.POST("/api/admin/v1/session", { body, signal }),
    options.onUnauthenticated,
  );
  const logout: AdminApi["logout"] = async (signal) => unwrapVoid(
    await client.DELETE("/api/admin/v1/session", { signal }),
    options.onUnauthenticated,
  );

  return buildAdminApi(client, { currentAdmin, login, logout }, options) satisfies AdminApi;
}
```

`buildAdminApi` implements every other `AdminApi` member using the exact method/path pair in the Stage Entry Contract, passes generated `params.path`, `params.query`, and `body` without renaming fields, and returns through `unwrap` or `unwrapVoid`. The `satisfies AdminApi` check makes a missing operation a compile failure. `unwrap` accepts only documented 2xx data, recognizes `application/problem+json`, invokes `onUnauthenticated` only for `401 unauthenticated`, returns a fixed `network_error` Problem for fetch failures, and never includes request bodies or raw response text. Validate all entity IDs at the adapter boundary with:

```ts
type AuthMethods = Pick<AdminApi, "currentAdmin" | "login" | "logout">;

declare function buildAdminApi(
  client: Client<paths>,
  auth: AuthMethods,
  options: AdminApiOptions,
): AdminApi;
```

```ts
export function requireEntityId(value: unknown, field: string): EntityId {
  if (typeof value !== "number" || !Number.isSafeInteger(value) || value <= 0) {
    throw new ApiProblem(502, "invalid_api_response", "client", `Invalid ${field}`);
  }
  return value;
}
```

- [ ] **Step 4: Add deterministic query keys and strict mock reset**

Define keys such as `queryKeys.me`, `queryKeys.articles(filters)`, `queryKeys.article(id)`, `queryKeys.versions(id)`, `queryKeys.publishJobs`, and three settings keys. Configure MSW to throw on unhandled requests, reset handlers after each test, and close the server after the suite.

- [ ] **Step 5: Verify generation, transport, and types**

Run:

```bash
cd admin
npm test -- --run src/api/admin-api.test.ts
npm run typecheck
npm run generate:api
git diff --exit-code -- src/api/generated/admin.ts
```

Expected: PASS and the second generation leaves the generated file byte-for-byte clean.

- [ ] **Step 6: Commit**

```bash
git add admin/src/api admin/src/test admin/package.json admin/package-lock.json
git commit -m "feat(admin): add generated api client"
```

### Task 3: Establish the Accessible Cold-Engineering Shell and Async States

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

**Interfaces:**
- Consumes: `ApiProblem`, TanStack `QueryClient`, and React Router.
- Produces: `AppProviders`, `AppShell`, reusable loading/empty/error components, and responsive navigation shared by all protected routes.

- [ ] **Step 1: Write failing shell and accessibility tests**

Test desktop navigation labels for Articles, Publishing, Site, Builder, and Hotlink; a mobile menu button with `aria-expanded`; a skip link; one `<main id="main-content">`; visible sanitized Problem text and request ID; `role="status"` on loading/save states; and zero serious axe violations.

```tsx
it("exposes one keyboard-navigable application shell", async () => {
  renderApp(<AppShell><h1>Articles</h1></AppShell>);
  expect(screen.getByRole("link", { name: "Skip to content" })).toHaveAttribute("href", "#main-content");
  expect(screen.getByRole("navigation", { name: "Admin" })).toBeInTheDocument();
  expect(screen.getByRole("main")).toHaveAttribute("id", "main-content");
  expect(await axe(document.body)).toHaveNoViolations();
});
```

- [ ] **Step 2: Run the test and observe missing shell components**

Run: `cd admin && npm test -- --run src/layout/AppShell.test.tsx`

Expected: FAIL because the shell, providers, and shared states do not exist.

- [ ] **Step 3: Implement providers and semantic shell**

Create one QueryClient with `retry: false` for mutations, at most one retry for queries except `401`, and no automatic refetch while an editor is dirty. `AppShell` uses semantic header/nav/main elements, route-aware `aria-current`, a keyboard-closeable mobile drawer, and a logout slot. `RouteErrorPage` renders a safe generic message and request ID without raw thrown values.

- [ ] **Step 4: Implement the visual tokens and responsive rules**

Define CSS custom properties for `--surface-0: #0d1117`, `--surface-1: #131a23`, `--text-1: #eef4fb`, `--text-2: #9eacbd`, `--accent: #2f9cff`, focus ring, borders, success/warning/error colors, spacing, radius, and a 72rem shell maximum. At widths below 48rem, replace the rail with the labelled menu control, stack page actions, maintain 44px targets, and keep editor/preview content within the viewport. Honor `prefers-reduced-motion` and never communicate status by color alone.

- [ ] **Step 5: Verify shell behavior**

Run:

```bash
cd admin
npm test -- --run src/layout/AppShell.test.tsx
npm run typecheck
```

Expected: PASS, including keyboard, semantic, reduced-motion class, and axe assertions.

- [ ] **Step 6: Commit**

```bash
git add admin/src/app admin/src/layout admin/src/components admin/src/styles admin/src/main.tsx admin/package.json admin/package-lock.json
git commit -m "feat(admin): add accessible application shell"
```

### Task 4: Implement Session Bootstrap, Login, Logout, and Protected Routing

**Files:**
- Create: `admin/src/auth/AuthProvider.tsx`
- Create: `admin/src/auth/RequireSession.tsx`
- Create: `admin/src/auth/LoginPage.tsx`
- Create: `admin/src/auth/auth.test.tsx`
- Modify: `admin/src/app/AppProviders.tsx`
- Modify: `admin/src/app/AppRouter.tsx`
- Modify: `admin/src/layout/AppShell.tsx`

**Interfaces:**
- Consumes: `AdminApi.login`, `logout`, `currentAdmin`, `AdminView`, `ApiProblem`, and the shared async states.
- Produces: `useAuth(): AuthContextValue`, `useAdminApi(): AdminApi`, `/login`, protected route layout, root redirect, and session-loss redirection.

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

- [ ] **Step 1: Write failing authentication-route tests**

Cover these cases with MSW: initial `/me` 200 renders the protected shell; `/me` 401 redirects to `/login` while preserving the intended pathname; `/me` 503 shows a retry state rather than the login form; successful login replaces `/login` with the intended route; wrong credentials retain the username, clear the password, and show the Problem request ID; logout calls DELETE and returns to login; no password or Session value appears in `localStorage`, `sessionStorage`, or the URL.

- [ ] **Step 2: Run the focused suite and observe missing authentication**

Run: `cd admin && npm test -- --run src/auth/auth.test.tsx`

Expected: FAIL because `AuthProvider`, `RequireSession`, and `LoginPage` are absent.

- [ ] **Step 3: Implement session bootstrap and protected routing**

`AuthProvider` constructs `createAdminApi({ onUnauthenticated })`, exposes that instance through `useAdminApi()`, and runs exactly one `currentAdmin()` query on startup. `RequireSession` returns a full-page status during bootstrap, `<Outlet />` only for authenticated state, `<Navigate to="/login" state={{ from: location.pathname }} replace />` for anonymous state, and a retryable dependency notice for service failure. The central callback clears only the Query cache and auth state; it never clears editor recovery content before the editor navigation guard runs.

Define routes exactly as approved. Every lazy page module exports `Component` so React Router can consume the module directly:

```tsx
createBrowserRouter([
  { path: "/login", element: <LoginPage /> },
  {
    element: <RequireSession />,
    children: [{
      element: <AppShell />,
      children: [
        { index: true, element: <Navigate to="/articles" replace /> },
        { path: "/articles", lazy: () => import("../articles/ArticleListPage") },
        { path: "/articles/new", lazy: () => import("../editor/ArticleEditorPage") },
        { path: "/articles/:articleId/edit", lazy: () => import("../editor/ArticleEditorPage") },
        { path: "/articles/:articleId/preview", lazy: () => import("../preview/ArticlePreviewPage") },
        { path: "/articles/:articleId/versions", lazy: () => import("../versions/ArticleVersionsPage") },
        { path: "/publishing", lazy: () => import("../publishing/PublishingPage") },
        { path: "/settings/site", lazy: () => import("../settings/SiteSettingsPage") },
        { path: "/settings/builder", lazy: () => import("../settings/BuilderSettingsPage") },
        { path: "/settings/hotlink", lazy: () => import("../settings/HotlinkSettingsPage") },
      ],
    }],
  },
]);
```

- [ ] **Step 4: Implement the hardened login form**

Use labels, `autocomplete="username"` and `autocomplete="current-password"`, a password input, disabled duplicate submission, an announced error, and focus the error summary after failure. Do not echo the password into component error state. On success, invalidate `queryKeys.me`; do not parse, copy, or persist `Set-Cookie`.

- [ ] **Step 5: Verify the complete auth boundary**

Run:

```bash
cd admin
npm test -- --run src/auth/auth.test.tsx
npm run typecheck
```

Expected: PASS for successful, rejected, unavailable, expired, and logout flows.

- [ ] **Step 6: Commit**

```bash
git add admin/src/auth admin/src/app admin/src/layout
git commit -m "feat(admin): add session protected routing"
```

### Task 5: Deliver the Real Article List and Lifecycle Actions

**Files:**
- Create: `admin/src/articles/ArticleListPage.tsx`
- Create: `admin/src/articles/ArticleListPage.test.tsx`
- Create: `admin/src/articles/article-actions.ts`
- Modify: `admin/src/app/AppRouter.tsx`
- Modify: `admin/src/api/query-keys.ts`

**Interfaces:**
- Consumes: `listArticles`, `createArticle`, `trashArticle`, `restoreArticle`, `unpublishArticle`, `ArticleSummary`, and `PublishJobView`.
- Produces: `/articles`, `/articles/new` navigation, confirmed lifecycle mutations, and invalidation of article/publishing queries.

- [ ] **Step 1: Write failing list-state and lifecycle tests**

Cover loading skeletons; empty active and trash filters; sanitized fetch failure with retry; cards/rows showing title, `draftUpdatedAt`, current publication state, and last publish result; new article creation; edit navigation; unpublished trash; published trash blocked with an “Unpublish first” explanation; confirmed unpublish creating a publish job; restore from trash; disabled actions during mutations; and a narrow-screen accessible action menu.

```tsx
it("does not trash a published article before successful unpublish", async () => {
  renderRoute("/articles", { articles: [publishedArticle] });
  await user.click(await screen.findByRole("button", { name: "Actions for Published post" }));
  expect(screen.getByRole("button", { name: "Move to trash" })).toBeDisabled();
  await user.click(screen.getByRole("button", { name: "Unpublish" }));
  await user.click(screen.getByRole("button", { name: "Confirm unpublish" }));
  expect(await screen.findByText("Publishing job queued")).toBeInTheDocument();
});
```

- [ ] **Step 2: Run the focused test and observe the missing list**

Run: `cd admin && npm test -- --run src/articles/ArticleListPage.test.tsx`

Expected: FAIL because article list routes and actions are absent.

- [ ] **Step 3: Implement query filters and explicit states**

Use URL query `state=active|trashed|all` and `page=<positive integer>` so refresh preserves the view. Sort display exactly as delivered by the service; never sort by ID. Render `Untitled draft` only when the contract title is empty. Use `<time dateTime>` for timestamps and text labels in every `StatusBadge`.

- [ ] **Step 4: Implement lifecycle mutations**

`createArticle({ title: "" })` navigates to `/articles/{id}/edit` only after validating its ID. Trash and restore require `ConfirmDialog`; successful mutations update or invalidate `queryKeys.articles`. Unpublish navigates to `/publishing?job=<id>` after success and leaves the article published until the job reports success. Never optimistically claim that public state changed.

- [ ] **Step 5: Verify article list behavior**

Run:

```bash
cd admin
npm test -- --run src/articles/ArticleListPage.test.tsx
npm run typecheck
```

Expected: PASS for active, published, trashed, empty, loading, failure, and mutation states.

- [ ] **Step 6: Commit**

```bash
git add admin/src/articles admin/src/app/AppRouter.tsx admin/src/api/query-keys.ts
git commit -m "feat(admin): add article lifecycle list"
```

### Task 6: Build the Milkdown Focus Canvas, Metadata, Whole-GFM Paste, and Source Mode

**Files:**
- Create: `admin/src/editor/editor-document.ts`
- Create: `admin/src/editor/editor-document.test.ts`
- Create: `admin/src/editor/milkdown-adapter.ts`
- Create: `admin/src/editor/MarkdownEditor.tsx`
- Create: `admin/src/editor/ArticleEditorPage.tsx`
- Create: `admin/src/editor/ArticleEditorPage.test.tsx`
- Create: `admin/src/styles/editor.css`
- Modify: `admin/src/app/AppRouter.tsx`

**Interfaces:**
- Consumes: `getArticleEditor`, `createArticle`, `listTags`, `ArticleEditorView`, `DraftRevision`, and approved GFM constraints.
- Produces: `EditorDocument`, `MilkdownAdapter`, `/articles/new`, `/articles/:articleId/edit`, visual/source modes, and one shared Markdown value.

```ts
export interface EditorDocument {
  title: string;
  summary: string;
  coverMediaId: EntityId | null;
  contentMd: string;
  tagNames: string[];
}

export interface MilkdownAdapter {
  mount(
    root: HTMLElement,
    initialMarkdown: string,
    callbacks: {
      onChange(markdown: string): void;
      onWholeDocumentPaste(markdown: string): void;
    },
  ): Promise<void>;
  replaceAll(markdown: string): void;
  insertMarkdown(markdown: string): void;
  focus(): void;
  destroy(): void;
}
```

- [ ] **Step 1: Write failing pure document and component tests**

Test immutable metadata updates, duplicate tag removal using trimmed case-insensitive comparison, conversion to `SaveArticleDraftRequest`, empty/new route creation, editor loading/failure, collapsed metadata disclosure, default visual mode, source toggle, source edits reflected after returning to visual mode, and whole-document paste containing headings, tasks, tables, strikethrough, links, images, inline code, and fenced code.

Use this exact fixed paste assertion:

```ts
const wholeGfm = `# Build log

- [x] booted
- [ ] deploy

| part | state |
| --- | --- |
| api | **ready** |

\`\`\`go
fmt.Println("ready")
\`\`\`
`;

expect(onWholeDocumentPaste).toHaveBeenLastCalledWith(wholeGfm);
expect(screen.getByRole("textbox", { name: "Markdown source" })).toHaveValue(wholeGfm);
```

- [ ] **Step 2: Run the focused tests and observe missing editor modules**

Run:

```bash
cd admin
npm test -- --run src/editor/editor-document.test.ts src/editor/ArticleEditorPage.test.tsx
```

Expected: FAIL because the document model and editor page do not exist.

- [ ] **Step 3: Implement the pure editor document boundary**

Implement `fromDraft`, `toSaveRequest`, `updateEditorDocument`, and `normalizeTagNames`. Draft autosave accepts an empty title so an untitled body is recoverable, but caps titles at 200 Unicode code points; Create Version and Publish separately require a nonempty trimmed title. Cap summary at 500 code points, allow at most 10 tags of 1–32 code points each, and reject case-insensitive `blob:` occurrences in `contentMd`. Keep validation messages associated with their controls; autosave preserves an over-limit or `blob:`-bearing local value but does not send it.

- [ ] **Step 4: Implement the real Milkdown adapter**

Use `@milkdown/kit/core`, `@milkdown/kit/preset/commonmark`, the table/task-list/strikethrough exports from `@milkdown/kit/preset/gfm`, `@milkdown/kit/plugin/listener`, `@milkdown/kit/plugin/clipboard`, and history. Compose `approvedGfmPlugins()` from only table, task-list, and strikethrough plugins; do not install the preset's footnote plugins. Add a contract test proving `[^1]` remains literal text. `listenerCtx.markdownUpdated` is the sole ordinary visual-to-string update path. A ProseMirror paste plugin detects `text/plain` block Markdown in an empty document, sends the exact clipboard string to `onWholeDocumentPaste`, parses that string into the canvas, and suppresses the immediate serializer echo so source mode preserves the pasted bytes. Ordinary selection paste remains normal editor insertion. Raw HTML is not enabled and unsupported controls are absent.

- [ ] **Step 5: Implement the focused page and mode synchronization**

The title is always visible. Summary, tags, and cover live in a `<details>` metadata panel. Visual mode unmounts cleanly before source mode displays a labelled monospace `<textarea>`; switching back calls `replaceAll(document.contentMd)` exactly once. The route toolbar exposes Preview and Versions destinations; Task 7 disables navigation to them until autosave is saved. Add a dirty marker as soon as any document field changes.

- [ ] **Step 6: Verify GFM and mode behavior**

Run:

```bash
cd admin
npm test -- --run src/editor/editor-document.test.ts src/editor/ArticleEditorPage.test.tsx
npm run typecheck
```

Expected: PASS; the same `contentMd` reaches visual, source, and save-request boundaries without HTML enablement.

- [ ] **Step 7: Commit**

```bash
git add admin/src/editor admin/src/styles/editor.css admin/src/app/AppRouter.tsx admin/package.json admin/package-lock.json
git commit -m "feat(admin): add milkdown article editor"
```

### Task 7: Add Race-Safe Two-Second Autosave and Explicit Conflict Recovery

**Files:**
- Create: `admin/src/editor/useAutosave.ts`
- Create: `admin/src/editor/useAutosave.test.tsx`
- Create: `admin/src/editor/ConflictDialog.tsx`
- Modify: `admin/src/editor/ArticleEditorPage.tsx`
- Modify: `admin/src/components/SaveIndicator.tsx`

**Interfaces:**
- Consumes: `AdminApi.saveArticleDraft`, `EditorDocument`, `DraftRevision.lockVersion`, and `ApiProblem`.
- Produces: `useAutosave(options): AutosaveResult`, save-state announcements, retry, copy-local, reload-server, and dirty-navigation protection.

```ts
export interface AutosaveOptions {
  articleId: EntityId;
  initial: EditorDocument;
  initialLockVersion: number;
  delayMs: 2000;
  save(input: SaveArticleDraftRequest): Promise<DraftRevision>;
  reload(): Promise<ArticleEditorView>;
}

export interface AutosaveResult {
  document: EditorDocument;
  state: SaveState;
  setDocument(next: EditorDocument): void;
  retry(): void;
  copyLocal(): Promise<void>;
  reloadServer(): Promise<void>;
  canLeave: boolean;
}
```

- [ ] **Step 1: Write failing fake-timer state-machine tests**

Prove: no save at 1,999 ms; one save at 2,000 ms; a new edit resets the timer; invalid local data is retained and not sent; only one request runs at a time; an edit during an in-flight save sends a second payload with the returned new lock version; an older response never replaces newer Markdown; a network failure becomes `failed` and preserves content; retry succeeds; `409 draft_conflict` stops timers and exposes copy/reload only; successful save becomes `saved`; unmount aborts timers without discarding data.

- [ ] **Step 2: Run the focused test and observe the missing hook**

Run: `cd admin && npm test -- --run src/editor/useAutosave.test.tsx`

Expected: FAIL because `useAutosave` and conflict recovery do not exist.

- [ ] **Step 3: Implement serialized generation-aware autosave**

Track a monotonically increasing local generation and one in-flight promise. Capture `{ generation, document, lockVersion }` per request. On success, adopt the returned lock version; mark saved only when the returned generation still equals the current generation, otherwise immediately schedule the latest valid document with the new lock. Map only `ApiProblem.code === "draft_conflict" && status === 409` to conflict; all other failures remain retryable.

- [ ] **Step 4: Implement conflict and navigation UX**

`SaveIndicator` announces `Saving`, `Saved`, `Save failed`, or `Version conflict` without flashing on every keystroke. `ConflictDialog` offers “Copy local Markdown” and “Reload server draft”; copying uses `navigator.clipboard.writeText(local.contentMd)` and leaves conflict state unchanged. Reload requires confirmation and replaces all fields only after GET succeeds. Use `beforeunload` and a React Router blocker whenever `canLeave` is false.

- [ ] **Step 5: Integrate autosave into the editor**

Use the service revision's `lockVersion`, display `updatedAt` only from successful responses, disable Preview, Versions, Create Version, and Publish while dirty/saving/failed/conflicted, and never display “Saved” for invalid data or a failed request.

- [ ] **Step 6: Verify timing, races, and recovery**

Run:

```bash
cd admin
npm test -- --run src/editor/useAutosave.test.tsx src/editor/ArticleEditorPage.test.tsx
npm run typecheck
```

Expected: PASS for every timer, in-flight edit, failure, conflict, copy, reload, and navigation case.

- [ ] **Step 7: Commit**

```bash
git add admin/src/editor admin/src/components/SaveIndicator.tsx
git commit -m "feat(admin): add conflict safe autosave"
```

### Task 8: Implement Policy-Based Direct Image Upload and Stable Markdown Insertion

**Files:**
- Create: `admin/src/media/image-upload.ts`
- Create: `admin/src/media/image-upload.test.ts`
- Create: `admin/src/media/useEditorImageUpload.ts`
- Modify: `admin/src/editor/MarkdownEditor.tsx`
- Modify: `admin/src/editor/ArticleEditorPage.tsx`

**Interfaces:**
- Consumes: `createMediaUploadPolicy`, external GFS multipart response, `registerMedia`, and `MilkdownAdapter.insertMarkdown`.
- Produces: `uploadEditorImage(file, dependencies): Promise<MediaView>`, progress/cancel state, paste/drop/file-picker entry points, and `/img/proxy/{publicKey}` insertion.

```ts
export interface GfsUploadResult {
  fileId: string;
  originalName: string;
  mimeType: string;
  fileSize: number;
  width: number;
  height: number;
}

export interface UploadImageDependencies {
  api: Pick<AdminApi, "createMediaUploadPolicy" | "registerMedia">;
  sendMultipart(
    url: string,
    fields: Record<string, string>,
    file: File,
    onProgress: (percent: number) => void,
    signal: AbortSignal,
  ): Promise<GfsUploadResult>;
}
```

- [ ] **Step 1: Write failing upload-chain tests**

Test rejection of non-image MIME, zero bytes, and files above the policy maximum; policy request before GFS; every returned policy field appended before the `file` part; direct request sent to `uploadUrl` rather than `/api`; actual GFS metadata passed to `registerMedia`; mismatch or malformed metadata rejected before registration; progress announced; abort does not register; sanitized errors omit URL query and policy values; successful insertion is `![escaped alt](/img/proxy/m_publicKey)` and never contains `blob:`, `fileId`, or a signed URL.

- [ ] **Step 2: Run the upload test and observe missing functions**

Run: `cd admin && npm test -- --run src/media/image-upload.test.ts`

Expected: FAIL because upload policy, multipart transport, and registration orchestration are absent.

- [ ] **Step 3: Implement direct multipart transport**

Use `XMLHttpRequest` only inside `sendMultipart` so upload progress is available. Set no manual multipart `Content-Type`; FormData supplies its boundary. On abort call `xhr.abort()`. Accept a 2xx JSON object only when all `GfsUploadResult` fields have correct primitive types and positive dimensions/size. Convert any external failure to a fixed `Image upload failed` message.

- [ ] **Step 4: Implement policy and registration orchestration**

Call `createMediaUploadPolicy({ originalName, mimeType, fileSize })`, verify `expiresAt` is in the future, compare the file against `allowedMimeTypes` and `maxFileSize`, upload directly, then call `registerMedia` with the returned actual metadata. Validate `MediaView.publicKey` against `^m_[A-Za-z0-9_-]{16,}$` and return only the registered media.

- [ ] **Step 5: Wire paste, drop, picker, cover, and cancellation**

Image files take precedence over text in editor paste/drop events. Insert at the Milkdown selection after registration; if the user changed to source mode, insert at the textarea selection. The metadata panel uses the same upload hook for cover and stores `coverMediaId` instead of Markdown. Show per-file progress, retry, and cancel; keep the document untouched on any failure.

- [ ] **Step 6: Verify upload boundaries**

Run:

```bash
cd admin
npm test -- --run src/media/image-upload.test.ts src/editor/ArticleEditorPage.test.tsx
npm run typecheck
```

Expected: PASS for paste, drop, picker, cover, direct transport, cancellation, sanitized failure, and stable URL insertion.

- [ ] **Step 7: Commit**

```bash
git add admin/src/media admin/src/editor
git commit -m "feat(admin): add direct image upload"
```

### Task 9: Create the Safe Full-Article Preview and Cross-Stage Markdown Contract

**Files:**
- Create: `contracts/markdown/article-content.css`
- Create: `contracts/markdown/fixtures/full-gfm.md`
- Create: `admin/src/preview/render-markdown.ts`
- Create: `admin/src/preview/render-markdown.test.ts`
- Create: `admin/src/preview/ArticlePreviewPage.tsx`
- Create: `admin/src/preview/ArticlePreviewPage.test.tsx`
- Modify: `admin/src/app/AppRouter.tsx`
- Modify: `admin/vite.config.ts`

**Interfaces:**
- Consumes: `getArticleEditor`, the saved draft, approved Markdown subset, and public origin `https://qiuxs.com`.
- Produces: `renderAdminPreview(markdown: string): Promise<RenderedArticle>`, shared article-content CSS/fixture for Stage 5, and protected `/articles/:articleId/preview`.

```ts
export interface RenderedArticle {
  html: string;
  headings: Array<{ depth: 2 | 3; id: string; text: string }>;
}

export async function renderAdminPreview(markdown: string): Promise<RenderedArticle>;
```

- [ ] **Step 1: Add the fixed full-GFM fixture and failing renderer tests**

The fixture contains every supported syntax, duplicate `##` headings, a fenced Go block with a language, an external HTTPS link, `/img/proxy/m_fixturePublicKey`, raw `<script>alert(1)</script>`, an `onclick` attribute, and a `javascript:` link. Tests assert GFM output exists; raw HTML/script/event attributes and unsafe protocols do not; duplicate heading IDs are `repeat` and `repeat-1`; external links have `target="_blank"` and `rel="noopener noreferrer"`; the image becomes `https://qiuxs.com/img/proxy/m_fixturePublicKey`; code has Shiki markup; and source Markdown is not mutated.

- [ ] **Step 2: Run the renderer test and observe the missing preview**

Run: `cd admin && npm test -- --run src/preview/render-markdown.test.ts`

Expected: FAIL because the renderer and shared fixture do not exist.

- [ ] **Step 3: Implement the explicit safe pipeline**

Use `remark-parse`, `remark-gfm`, `remark-rehype` without `allowDangerousHtml`, `rehype-sanitize` with an explicit schema for approved elements/attributes, a deterministic GitHub-style slugger, a small AST transform for external links and public image URLs, `@shikijs/rehype` with one dark and one light theme, and `rehype-stringify`. Do not use `rehype-raw`. Rewrite only image paths matching `^/img/proxy/m_[A-Za-z0-9_-]+$`; remove other image sources from preview output. Extract only level-two and level-three headings for the preview table of contents.

- [ ] **Step 4: Implement the protected full preview route**

Load the saved draft through `getArticleEditor`; do not use an editor singleton or trigger a save. The editor prevents Preview navigation while unsaved, and a directly opened Preview route always shows the last server-saved draft. Render title, summary, tags, cover, article metadata, table of contents, and complete body. Lazy-load the renderer bundle only on the preview route and show loading, rendering, retryable fetch failure, and rendering failure states.

- [ ] **Step 5: Share the article-content style contract**

Put typography, tables, blockquotes, task lists, links, images, inline code, code blocks, and heading anchor styles in `contracts/markdown/article-content.css`. Configure Vite to allow a read-only import from the repository-level `contracts/` directory. Stage 5 will import the same file and fixture; Admin-specific chrome remains in `admin/src/styles/`.

- [ ] **Step 6: Verify safety and route behavior**

Run:

```bash
cd admin
npm test -- --run src/preview/render-markdown.test.ts src/preview/ArticlePreviewPage.test.tsx
npm run typecheck
npm run build
npm run verify:dist
```

Expected: PASS; unsupported/dangerous input is inert, images target the public domain, and preview output is in the lazy route chunk.

- [ ] **Step 7: Commit**

```bash
git add contracts/markdown admin/src/preview admin/src/app/AppRouter.tsx admin/vite.config.ts admin/package.json admin/package-lock.json
git commit -m "feat(admin): add safe article preview"
```

### Task 10: Add Manual Versions, Immutable History, and Copy-on-Restore

**Files:**
- Create: `admin/src/versions/ArticleVersionsPage.tsx`
- Create: `admin/src/versions/ArticleVersionsPage.test.tsx`
- Modify: `admin/src/editor/ArticleEditorPage.tsx`
- Modify: `admin/src/app/AppRouter.tsx`
- Modify: `admin/src/api/query-keys.ts`

**Interfaces:**
- Consumes: `listArticleVersions`, `createArticleVersion`, `restoreArticleVersion`, autosave `SaveState`, and `ArticleVersionPage`.
- Produces: `/articles/:articleId/versions`, safe Create Version, confirmed restore, and editor cache replacement with the new draft.

- [ ] **Step 1: Write failing history and restore tests**

Cover loading, no manual versions, retryable failure, ordering by `createdAt` then ID as delivered by the API, labels for `manual_version` and `publish_snapshot`, immutable title/summary/tag snapshots, Create Version disabled unless autosave is `saved`, Create Version returning a new editing draft, restore confirmation naming the source revision, restore returning a new draft/revision ID, and no mutation of the history row.

- [ ] **Step 2: Run the focused test and observe missing version UI**

Run: `cd admin && npm test -- --run src/versions/ArticleVersionsPage.test.tsx`

Expected: FAIL because version routes and mutations are absent.

- [ ] **Step 3: Implement version queries and display**

Use semantic ordered history, `<time>` timestamps, reason/status text, and stable pagination from the contract. The route ID and every returned revision ID pass `requireEntityId`. Do not infer chronology from IDs.

- [ ] **Step 4: Implement create and restore boundaries**

Create Version is reachable from the editor only when there are no unsaved changes. On success replace `queryKeys.article(articleId)` with the returned `ArticleEditorView`, invalidate versions, and keep the user in the editor. Restore requires confirmation, calls the copy-on-restore endpoint, updates the editor cache with the returned new editing revision, and navigates to edit; never PATCH a historical revision.

- [ ] **Step 5: Verify versions**

Run:

```bash
cd admin
npm test -- --run src/versions/ArticleVersionsPage.test.tsx src/editor/ArticleEditorPage.test.tsx
npm run typecheck
```

Expected: PASS for immutable display, save gating, copy creation, restore, loading, empty, and failure states.

- [ ] **Step 6: Commit**

```bash
git add admin/src/versions admin/src/editor/ArticleEditorPage.tsx admin/src/app/AppRouter.tsx admin/src/api/query-keys.ts
git commit -m "feat(admin): add article version history"
```

### Task 11: Implement Article and Whole-Site Publishing Status

**Files:**
- Create: `admin/src/publishing/publish-status.ts`
- Create: `admin/src/publishing/PublishingPage.tsx`
- Create: `admin/src/publishing/PublishingPage.test.tsx`
- Modify: `admin/src/editor/ArticleEditorPage.tsx`
- Modify: `admin/src/articles/ArticleListPage.tsx`
- Modify: `admin/src/app/AppRouter.tsx`
- Modify: `admin/src/api/query-keys.ts`

**Interfaces:**
- Consumes: article publish/unpublish methods, `listPublishJobs`, `getPublishJob`, `retryPublishJob`, `terminatePublishJob`, `publishSite`, and `PublishJobView`.
- Produces: editor publish action, `/publishing`, active-job polling, retry/terminate, and status text that never predicts deployment success.

```ts
export function isActivePublishStatus(status: PublishJobStatus): boolean {
  return ["pending", "queued", "building", "deploying"].includes(status);
}

export function publishStatusLabel(job: PublishJobView): string;
```

- [ ] **Step 1: Write failing publishing-state tests**

Test all six statuses, current stage/build number when present, sanitized error summary, request ID on API failures, three-second polling only while a job is active, polling stop on success/failure/unmount, global `publish_in_progress`, article Publish disabled until autosave is saved, publish confirmation, site publish confirmation, retry creating a different job ID for the same Release, terminate available only to active jobs, and no UI update of public/published state until a successful job response arrives.

- [ ] **Step 2: Run the focused test and observe missing publishing modules**

Run: `cd admin && npm test -- --run src/publishing/PublishingPage.test.tsx`

Expected: FAIL because publishing status, route, and mutations do not exist.

- [ ] **Step 3: Implement status mapping and polling**

Map every status to text plus a non-color icon. Poll `getPublishJob(id)` every 3,000 ms only for active jobs; the list query itself does not poll. Keep the server's Release ID and job ID distinct. Display only the contract's cropped `errorSummary`; never fetch Jenkins logs or render arbitrary HTML.

- [ ] **Step 4: Implement publish, retry, and termination actions**

Article publish confirmation states that the saved draft will be frozen and current editing can continue after queueing. Site publish confirmation states that current online articles plus latest saved site settings form the Release. Retry preserves the failed immutable Release and selects the returned new job. Terminate requires confirmation and does not claim rollback. On `publish_in_progress`, focus and link to the active job.

- [ ] **Step 5: Verify publishing behavior**

Run:

```bash
cd admin
npm test -- --run src/publishing/PublishingPage.test.tsx src/editor/ArticleEditorPage.test.tsx src/articles/ArticleListPage.test.tsx
npm run typecheck
```

Expected: PASS for lifecycle status, polling, global lock, failure, retry, termination, article publish, unpublish, and site publish.

- [ ] **Step 6: Commit**

```bash
git add admin/src/publishing admin/src/editor/ArticleEditorPage.tsx admin/src/articles/ArticleListPage.tsx admin/src/app/AppRouter.tsx admin/src/api/query-keys.ts
git commit -m "feat(admin): add publishing controls"
```

### Task 12: Build Site, SEO, Social, About, and ICP Settings

**Files:**
- Create: `admin/src/settings/settings-validation.ts`
- Create: `admin/src/settings/SiteSettingsPage.tsx`
- Create: `admin/src/settings/SiteSettingsPage.test.tsx`
- Modify: `admin/src/app/AppRouter.tsx`
- Modify: `admin/src/api/query-keys.ts`

**Interfaces:**
- Consumes: `getSiteSettings`, `updateSiteSettings`, `publishSite`, `SiteSettingsView`, and `UpdateSiteSettingsRequest`.
- Produces: `/settings/site`, field-level validation, saved-versus-published status, and explicit Publish Site action.

- [ ] **Step 1: Write failing site-settings tests**

Cover loading/failure/retry; site name, author introduction, home status, About Markdown, ordered social links, SEO title/description, optional default OG media, filing name, and filing number; initial filing values `长安休息室` and `浙ICP备17057726号-1`; required filing fields; safe `https` social links; Add/Remove/Reorder social links; optimistic-lock submission; dirty navigation warning; save success retaining “Pending publication”; save conflict; and separate Publish Site creating a job.

- [ ] **Step 2: Run the focused test and observe missing settings**

Run: `cd admin && npm test -- --run src/settings/SiteSettingsPage.test.tsx`

Expected: FAIL because site settings and validation do not exist.

- [ ] **Step 3: Implement pure validation and normalization**

`validateSiteSettings` trims scalar fields, requires nonempty site/filing values, limits site name to 100 code points, author/home/SEO text to contract limits, preserves About Markdown, accepts social URLs only when `new URL(value).protocol === "https:"`, keeps user ordering, removes exact duplicate URLs, and includes `lockVersion` from the loaded view.

- [ ] **Step 4: Implement save and publish separation**

Use an accessible form with field summaries, mark dirty on changes, disable duplicate saves, and update the Query cache only with the server response. After save, render “Saved settings — pending publication”; do not say the public site changed. Publish Site is a second confirmed mutation that navigates to `/publishing?job=<id>`. A conflict keeps local fields and offers “Copy local settings JSON” plus confirmed “Reload server settings”; copying serializes only the public site form fields and never includes a secret.

- [ ] **Step 5: Verify site settings**

Run:

```bash
cd admin
npm test -- --run src/settings/SiteSettingsPage.test.tsx
npm run typecheck
```

Expected: PASS for validation, defaults, social editing, save/conflict, dirty guard, pending state, and publish action.

- [ ] **Step 6: Commit**

```bash
git add admin/src/settings/settings-validation.ts admin/src/settings/SiteSettingsPage.tsx admin/src/settings/SiteSettingsPage.test.tsx admin/src/app/AppRouter.tsx admin/src/api/query-keys.ts
git commit -m "feat(admin): add site settings"
```

### Task 13: Build Secret-Safe Jenkins Builder Settings and Connection Test

**Files:**
- Create: `admin/src/settings/BuilderSettingsPage.tsx`
- Create: `admin/src/settings/BuilderSettingsPage.test.tsx`
- Modify: `admin/src/settings/settings-validation.ts`
- Modify: `admin/src/app/AppRouter.tsx`

**Interfaces:**
- Consumes: `getBuilderSettings`, `updateBuilderSettings`, `testBuilderConnection`, and the token-redacted `BuilderSettingsView`.
- Produces: `/settings/builder`, HTTPS URL validation, blank-token preservation, and safe connection-result display.

- [ ] **Step 1: Write failing builder security tests**

Cover redacted load with `hasToken`; fields for name, HTTPS base URL, username, password-style API Token, Job Name, and enabled; reject HTTP, userinfo, query, fragment, and non-root path; blank token omitted from update; nonblank token included once then immediately cleared from React state/DOM; no token in cache, storage, URL, thrown text, MSW diagnostics, or connection result; disable Test Connection while unsaved; sanitized success/failure; and optimistic-lock conflict.

- [ ] **Step 2: Run the focused test and observe missing builder settings**

Run: `cd admin && npm test -- --run src/settings/BuilderSettingsPage.test.tsx`

Expected: FAIL because the builder form and secure update semantics are absent.

- [ ] **Step 3: Implement strict builder validation**

Normalize `baseUrl` to `https://host[:port]` only. Require name, username, and Job Name when enabled. Construct the mutation body with a conditional spread so `token` is absent when the input is blank:

```ts
const request: UpdateBuilderSettingsRequest = {
  name,
  baseUrl: normalizeBuilderUrl(baseUrl),
  username,
  jobName,
  enabled,
  lockVersion,
  ...(token === "" ? {} : { token }),
};
```

- [ ] **Step 4: Implement save and connection-test UX**

Use `autocomplete="new-password"` for Token and explain “Leave blank to keep the stored token.” On successful update, replace the view with the redacted response and clear the input. Connection Test is enabled only when the current form exactly matches the saved redacted configuration and `enabled` is true. Render only `BuilderConnectionResult.success` and its cropped service message.

- [ ] **Step 5: Verify builder security**

Run:

```bash
cd admin
npm test -- --run src/settings/BuilderSettingsPage.test.tsx src/api/admin-api.test.ts
npm run typecheck
```

Expected: PASS, including a recursive DOM/storage/cache scan that finds no submitted Token value.

- [ ] **Step 6: Commit**

```bash
git add admin/src/settings/BuilderSettingsPage.tsx admin/src/settings/BuilderSettingsPage.test.tsx admin/src/settings/settings-validation.ts admin/src/app/AppRouter.tsx
git commit -m "feat(admin): add builder settings"
```

### Task 14: Build Immediately Effective Hotlink Settings

**Files:**
- Create: `admin/src/settings/HotlinkSettingsPage.tsx`
- Create: `admin/src/settings/HotlinkSettingsPage.test.tsx`
- Modify: `admin/src/settings/settings-validation.ts`
- Modify: `admin/src/app/AppRouter.tsx`

**Interfaces:**
- Consumes: `getHotlinkSettings`, `updateHotlinkSettings`, `HotlinkSettingsView`, default hosts, and `allowEmptyReferer`.
- Produces: `/settings/hotlink`, exact-host normalization, add/enable/disable/delete behavior, and immediate-effective confirmation.

- [ ] **Step 1: Write failing hotlink behavior tests**

Cover loading/failure/retry; default enabled hosts `qiuxs.com` and `blog-admin.qiuxs.com`; `allowEmptyReferer` initially true; add a hostname; lowercase/trailing-dot normalization; reject schemes, paths, ports, wildcards, credentials, whitespace, IP literals, and duplicate normalized hosts; enable/disable existing rows; delete confirmation; optimistic-lock save; conflict retention; and success text that says the Go image proxy updates immediately without publishing the static site.

- [ ] **Step 2: Run the focused test and observe missing hotlink settings**

Run: `cd admin && npm test -- --run src/settings/HotlinkSettingsPage.test.tsx`

Expected: FAIL because hotlink settings and host normalization are absent.

- [ ] **Step 3: Implement exact hostname normalization**

```ts
export function normalizeAllowedHostname(raw: string): string {
  const candidate = raw.trim().toLowerCase().replace(/\.$/, "");
  if (
    candidate === "" ||
    candidate.includes(":") ||
    candidate.includes("/") ||
    candidate.includes("*") ||
    /^\d+\.\d+\.\d+\.\d+$/.test(candidate) ||
    !/^(?=.{1,253}$)([a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])$/.test(candidate)
  ) {
    throw new Error("Enter an exact DNS hostname without scheme, path, port, or wildcard");
  }
  return candidate;
}
```

Keep the backend authoritative: the client validates for fast feedback, then renders any safe Problem returned by the service. IDs on existing allowlist rows still pass `requireEntityId`.

- [ ] **Step 4: Implement the accessible allowlist editor**

Use a table on wide screens and labelled cards on narrow screens. Each row has an explicit enabled checkbox and Delete button naming the host. Save the entire normalized list plus `allowEmptyReferer` and `lockVersion` in one request. On success replace the form/cache with the server response and announce “Image hotlink rules are active now”; do not offer Publish Site.

- [ ] **Step 5: Verify hotlink settings**

Run:

```bash
cd admin
npm test -- --run src/settings/HotlinkSettingsPage.test.tsx
npm run typecheck
```

Expected: PASS for default, add, normalize, duplicate, enable, disable, delete, immediate save, conflict, and responsive semantics.

- [ ] **Step 6: Commit**

```bash
git add admin/src/settings/HotlinkSettingsPage.tsx admin/src/settings/HotlinkSettingsPage.test.tsx admin/src/settings/settings-validation.ts admin/src/app/AppRouter.tsx
git commit -m "feat(admin): add hotlink settings"
```

### Task 15: Prove the Key Browser Flows Without a Real Backend

**Files:**
- Create: `admin/playwright.config.ts`
- Create: `admin/e2e/support/mock-admin-api.ts`
- Create: `admin/e2e/auth-editor.spec.ts`
- Create: `admin/e2e/conflict-upload-publish.spec.ts`
- Create: `admin/e2e/settings-responsive.spec.ts`
- Modify: `admin/package.json`
- Modify: `admin/package-lock.json`

**Interfaces:**
- Consumes: the complete built SPA, exact OpenAPI-shaped fixtures, and Playwright `page.route`.
- Produces: Chromium browser acceptance for authentication, editing, preview, upload, conflict, versions, publishing, settings, accessibility smoke, and narrow-screen layout with zero real network dependencies.

- [ ] **Step 1: Write a strict stateful mock API**

`installMockAdminApi(page, initialState)` owns an in-memory authenticated flag, article/draft/lock version, versions, media, settings, and publish jobs. It fulfills every same-origin Admin endpoint with the exact contract content type and rejects every unregistered request. It also intercepts `https://gfs.test/v1/upload`, verifies multipart contains the policy field and file bytes, and returns deterministic actual metadata. Expose controls to force the next save to return `409 draft_conflict` or the next request to return a sanitized `503`.

```ts
export interface MockAdminState {
  authenticated: boolean;
  article: ArticleEditorView;
  versions: ArticleVersionPage;
  publishJobs: PublishJobView[];
  siteSettings: SiteSettingsView;
  builderSettings: BuilderSettingsView;
  hotlinkSettings: HotlinkSettingsView;
}

export async function installMockAdminApi(
  page: Page,
  state: MockAdminState,
): Promise<MockAdminController>;
```

- [ ] **Step 2: Write the failing login/editor/preview browser flow**

The first spec starts anonymous, opens a protected editor URL, signs in, verifies return to the editor, pastes `contracts/markdown/fixtures/full-gfm.md` into an empty Milkdown canvas, advances real time until the two-second save completes, reloads the page, confirms saved Markdown survives, switches to source and back, creates a manual version, opens full preview, verifies highlighted code and public-domain image URL, and logs out.

- [ ] **Step 3: Write the failing conflict/upload/publish browser flow**

The second spec pastes an in-memory PNG through a clipboard `DataTransfer`, verifies the policy call precedes the external GFS call and media registration, observes progress and stable Markdown insertion, forces a save conflict, copies local Markdown, reloads the server draft, saves again, publishes the article, observes queued/building/deploying/success transitions from the stateful mock, and confirms the UI never reports public success before the final status.

- [ ] **Step 4: Write the failing responsive settings flow**

The third spec uses a 390-by-844 viewport, opens and keyboard-operates the mobile navigation, updates site/ICP settings then separately publishes the site, submits Builder settings with a new token and proves it disappears from DOM/storage/URL after the response, tests the saved builder connection, updates the hotlink allowlist without publishing, and runs an axe scan on login, editor, publishing, and each settings page.

- [ ] **Step 5: Run the browser suite and observe failures before completing mocks/config**

Run:

```bash
cd admin
npm run build
npx playwright install chromium
npm run test:e2e
```

Expected before completion: FAIL on missing Playwright configuration, route handlers, or unmet UI behavior; no request may escape to a real service.

- [ ] **Step 6: Configure and satisfy the isolated browser suite**

Configure `webServer.command` as `npm run build && npm run preview -- --host 127.0.0.1 --port 4173`, `reuseExistingServer: false`, base URL `http://127.0.0.1:4173`, Chromium only, trace on first retry, screenshot only on failure, and a 30-second test timeout. Add a catch-all route that aborts and fails the test for any host other than `127.0.0.1:4173`, `gfs.test`, or the mocked public image request.

- [ ] **Step 7: Verify the browser acceptance gate**

Run:

```bash
cd admin
npm run test:e2e -- e2e/auth-editor.spec.ts
npm run test:e2e -- e2e/conflict-upload-publish.spec.ts
npm run test:e2e -- e2e/settings-responsive.spec.ts
```

Expected: all flows PASS without a Go process, container, database, Redis, Jenkins, GFS, or OSS connection.

- [ ] **Step 8: Commit**

```bash
git add admin/playwright.config.ts admin/e2e admin/package.json admin/package-lock.json
git commit -m "test(admin): cover browser management flows"
```

### Task 16: Finalize Operator Documentation and the Host-Build Artifact Gate

**Files:**
- Create: `admin/Makefile`
- Create: `admin/README.md`
- Modify: `admin/scripts/verify-dist.mjs`
- Modify: `admin/package.json`
- Modify: `README.md`

**Interfaces:**
- Consumes: every Admin test/build script and the Stage 6 versioned static deployment target.
- Produces: `make install`, `make generate`, `make test`, `make e2e`, `make build`, documented `admin/dist/` contract, and the Stage 4 completion gate.

- [ ] **Step 1: Write the failing final artifact assertions**

Extend `verify-dist.mjs` tests so it rejects missing `index.html`, unhashed JS/CSS, source maps, absolute local filesystem paths, backend/Jenkins/GFS secret names, any output file above 2 MiB, and HTML that references assets absent from `dist/`. Bundle only plaintext, Bash, JSON, YAML, Go, JavaScript, TypeScript, JSX, TSX, SQL, HTML, CSS, and Markdown preview grammars; render any other fence as plaintext so Shiki remains in the limit. The verifier accepts SPA routes being absent as physical HTML files because Stage 6 owns Nginx `try_files` fallback.

- [ ] **Step 2: Run the artifact test and observe missing final checks**

Run: `cd admin && node --test scripts/require-node.test.mjs && npm run build && npm run verify:dist`

Expected before completion: FAIL until the verifier and production chunking satisfy the artifact contract.

- [ ] **Step 3: Add the exact Make targets**

```make
.PHONY: version-check install generate test e2e build

version-check:
	@test "$$(node --version)" = "v20.19.4" || (echo "Node 20.19.4 required, got $$(node --version)" && exit 1)

install: version-check
	npm ci

generate: version-check
	npm run generate:api

test: version-check
	npm run typecheck
	npm run test:run

e2e: version-check
	npm run test:e2e

build: version-check test
	npm run generate:api
	git diff --exit-code -- src/api/generated/admin.ts
	npm run build
	npm run verify:dist
```

Ensure the Vite build uses hashed assets, splits Milkdown and preview/Shiki into lazy chunks, emits no source maps, and has no runtime dependency on Node or the Go service during initial static file loading.

- [ ] **Step 4: Document development, verification, and Stage 6 handoff**

`admin/README.md` must document Node 20.19.4 setup, `npm ci`, API generation, local same-origin proxy, unit tests, isolated Playwright tests, build commands, all Admin routes, the fixed public preview image origin, and that the output is the complete `dist/` directory. State that Stage 6 must deploy the whole directory to a new `/web/deploy/blog-admin/releases/<revision>` path, verify `index.html` plus hashed assets, configure history fallback for protected SPA routes, and atomically switch `current`; do not add those Jenkins/Nginx files here. Explain that no runtime environment file or secret is needed because API calls are same-origin.

- [ ] **Step 5: Run the complete clean Stage 4 gate**

Run from a clean checkout with no service process:

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
npm run test:e2e
git diff --check
```

Expected: every command PASS; tests make no real network connection; `dist/` is ignored and contains a host-built static SPA ready for the Stage 6 atomic deployment pipeline. Only Task 16 documentation/build-gate files remain uncommitted before Step 7.

- [ ] **Step 6: Perform the manual responsive/accessibility smoke**

With `npm run dev`, inspect login, article list, editor visual/source modes, preview, versions, publishing, and all settings routes at 1440-by-900 and 390-by-844. Complete every flow by keyboard, confirm focus remains visible, status changes are announced, long Markdown/code/tables do not force page-wide horizontal scrolling, errors retain local input, and the cold-engineering hierarchy remains readable in both viewport sizes.

- [ ] **Step 7: Commit**

```bash
git add admin/Makefile admin/README.md admin/scripts/verify-dist.mjs admin/package.json README.md
git commit -m "docs(admin): define build and deployment artifact"
```

- [ ] **Step 8: Confirm the implementation branch is clean**

Run: `git status --short`

Expected: no output. Ignored `admin/dist/`, Playwright results, coverage, and dependencies do not appear.

---

## Stage 4 Completion Gate

Stage 4 is complete only when all sixteen task commits are independently reviewable and the Task 16 clean gate passes on Node.js exactly `20.19.4`. The browser acceptance must prove login, whole-GFM paste, two-second autosave, conflict recovery, direct image upload, source/preview, version creation/restoration, article and site publishing, and all three settings areas without a real backend. The committed output is source, tests, shared Markdown contracts, documentation, and lockfile; `admin/dist/` is reproducible and ignored. Jenkins job definitions, rsync, `/web/deploy/blog-admin` release directories, Nginx proxy/history fallback, atomic symlink switching, and deployed-environment smoke tests remain exclusively in Stage 6.
