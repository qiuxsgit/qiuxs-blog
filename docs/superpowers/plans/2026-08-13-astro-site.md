# Astro Static Site Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the complete Astro 6 public blog from an immutable Release Bundle as static HTML, with the approved visual system, content routes, search, feeds, SEO,备案 information, and no SSR or runtime content API.

**Architecture:** `site/` accepts a schema-versioned gzip/JSON bundle at build time, validates and normalizes it once, and exposes typed collections to Astro static routes. Markdown is rendered only during build with raw HTML disabled and Shiki highlighting; small browser scripts provide progressive enhancement. A deterministic fixture and output inspector make the deployment gate testable without Service, MySQL, Redis, or network access.

**Tech Stack:** Astro 6, Node.js 22.20.0 Docker image, TypeScript, pnpm lockfile, Zod, unified/remark GFM, Shiki, Pagefind, Vitest, Playwright screenshot tests.

## Global Constraints

- `site/` builds only inside the pinned Node.js `22.20.0` container and uses `pnpm install --frozen-lockfile`.
- Output is fully static; no server adapter, SSR route, runtime content API, or browser-side Markdown parsing is allowed.
- Bundle schema version is `1`; all IDs are JSON-safe signed `int64` decimal strings at this boundary.
- Raw HTML, unsafe URL protocols, MDX, Mermaid, formulas, footnotes, and custom components are rejected.
- Every public template renders non-empty `filing_name` and `filing_number`, with the number linking to `https://beian.miit.gov.cn/`.
- Image content keeps stable `/img/proxy/{publicKey}` URLs.
- Build output must include `/`, posts, tags, archive, about, 404, RSS, sitemap, Pagefind, OG assets, and `release.json`.
- Tests do not call the live Service or internet.

---

## Planned File Map

- `contracts/release-bundle.schema.json`: language-neutral bundle contract.
- `contracts/fixtures/release-bundle.v1.json`: deterministic full-content fixture.
- `contracts/fixtures/markdown-gfm.md`: shared Markdown acceptance sample.
- `site/src/lib/bundle.ts`: load, validate, checksum, normalize, and expose bundle data.
- `site/src/lib/markdown.ts`: safe build-time Markdown pipeline and heading metadata.
- `site/src/lib/content.ts`: reading metrics, navigation, tags, archives, and URLs.
- `site/src/layouts/BaseLayout.astro`, `ArticleLayout.astro`: shared document and article shells.
- `site/src/components/*`: header, footer, article cards, TOC, theme/search/copy UI.
- `site/src/pages/*`: all required static routes and XML endpoints.
- `site/src/styles/*`: approved cold engineering-log tokens and responsive typography.
- `site/scripts/inspect-output.mjs`: deployment artifact gate.
- `site/tests/*`: contract, Markdown, route, SEO,备案, and visual tests.
- `site/Dockerfile`, `site/Makefile`: exact containerized build entrypoints.

### Task 1: Scaffold the Deterministic Static Build Contract

**Files:** Create `site/package.json`, `site/pnpm-lock.yaml`, `site/astro.config.mjs`, `site/tsconfig.json`, `site/vitest.config.ts`, `contracts/release-bundle.schema.json`, `contracts/fixtures/release-bundle.v1.json`, `contracts/fixtures/markdown-gfm.md`, `site/src/lib/bundle.ts`, `site/tests/bundle.test.ts`.

**Interfaces:** Produces `loadBundle(path: string): Promise<ReleaseBundle>` and exported Zod-inferred bundle types; consumes schema version `1`, `site`, `tags`, `articles`, `checksum`.

- [ ] Write tests that accept the fixture, reject unknown schema versions/empty备案/duplicate slugs/unsafe IDs, and detect checksum changes.
- [ ] Run `corepack pnpm --dir site test -- bundle.test.ts` and observe missing loader failures.
- [ ] Scaffold Astro in static mode, implement strict schemas and canonical JSON SHA-256 validation, and resolve `BLOG_BUNDLE_PATH` with fixture fallback only in tests.
- [ ] Run the focused test and `corepack pnpm --dir site exec astro check`; both pass.
- [ ] Commit with `feat(site): validate immutable release bundles`.

### Task 2: Render Safe GFM and Content Metadata at Build Time

**Files:** Create `site/src/lib/markdown.ts`, `site/src/lib/content.ts`, `site/tests/markdown.test.ts`, `site/tests/content.test.ts`.

**Interfaces:** Produces `renderMarkdown(markdown): Promise<{html, headings, wordCount, readingMinutes}>`, `buildContentIndex(bundle)`, and stable post/tag/archive URLs.

- [ ] Test GFM tables/tasks/strikethrough, duplicate heading IDs, fenced code languages, safe external-link attributes, image proxy retention, and rejection/removal of raw HTML plus `javascript:`/`data:` URLs.
- [ ] Test deterministic word counts, reading time, adjacent posts, tag aggregation, and year archives ordered by explicit publish timestamps.
- [ ] Run focused Vitest files and observe failures.
- [ ] Implement unified remark-gfm → sanitized hast → Shiki build-time rendering and pure metadata helpers.
- [ ] Run focused tests and commit `feat(site): render safe technical markdown`.

### Task 3: Build the Shared Visual System and Shell

**Files:** Create `site/src/styles/tokens.css`, `site/src/styles/global.css`, `site/src/layouts/BaseLayout.astro`, `site/src/components/SiteHeader.astro`, `SiteFooter.astro`, `ThemeToggle.astro`, `SearchLauncher.astro`, `site/tests/layout.test.ts`.

**Interfaces:** `BaseLayout` requires title/description/canonical/OG props and always renders header, main landmark, footer备案, pre-paint theme script, and metadata.

- [ ] Test semantic landmarks, keyboard navigation, no-flash theme bootstrap, canonical/OpenGraph/Twitter defaults, and visible备案 link in representative rendered HTML.
- [ ] Observe failing component tests, then implement charcoal/cold-white/electric-blue tokens, restrained grid, accessible focus styles, desktop/mobile spacing, and light/dark themes.
- [ ] Run tests plus `astro check`; commit `feat(site): add engineering log visual shell`.

### Task 4: Generate Home, Lists, Tags, Archive, About, and 404

**Files:** Create `site/src/pages/index.astro`, `posts/index.astro`, `tags/index.astro`, `tags/[slug].astro`, `archive/index.astro`, `about/index.astro`, `404.astro`, `site/src/components/ArticleCard.astro`, `TagList.astro`, `site/tests/routes.test.ts`.

**Interfaces:** All routes consume `buildContentIndex`; every dynamic route implements `getStaticPaths`; every page uses `BaseLayout`.

- [ ] Add route tests asserting exact output paths, no cover dependency, stable empty states,备案 on every page type, and no runtime fetch calls.
- [ ] Observe failures, implement the pages with personal intro/recent posts/sidebar, tag and year indexes, rendered about Markdown, and useful 404 navigation.
- [ ] Build with the fixture and assert files exist; commit `feat(site): generate public index routes`.

### Task 5: Generate Article Pages and Progressive Enhancements

**Files:** Create `site/src/pages/posts/[slug].astro`, `site/src/layouts/ArticleLayout.astro`, `site/src/components/TableOfContents.astro`, `ReadingProgress.astro`, `CodeCopy.astro`, `site/src/scripts/article.ts`, `site/tests/article.test.ts`.

**Interfaces:** Article layout receives pre-rendered HTML/headings/metrics/previous/next; client script only enhances theme, progress, copy, and active TOC.

- [ ] Test title/summary/dates/tags/metrics, desktop TOC, previous/next, BlogPosting JSON-LD, safe highlighted code, proxy images, and that article body exists without JavaScript.
- [ ] Observe failures, implement article pages and small dependency-free enhancement script with accessible copied-state announcements.
- [ ] Run tests/build and commit `feat(site): generate technical article pages`.

### Task 6: Add Search, Feeds, Sitemap, and OG Assets

**Files:** Create `site/src/pages/rss.xml.ts`, `sitemap-index.xml.ts`, `site/src/pages/og/[slug].svg.ts`, `site/src/components/SearchPanel.astro`, `site/src/scripts/search.ts`, modify `site/package.json`, `site/astro.config.mjs`, add `site/tests/discovery.test.ts`.

**Interfaces:** Build command runs Astro then Pagefind; RSS/Sitemap/OG are deterministic bundle-derived static outputs; search lazily imports `/pagefind/pagefind.js`.

- [ ] Test RSS escaping/full canonical URLs, sitemap required routes, default SVG OG content, and Pagefind data attributes.
- [ ] Observe failures, implement endpoints and search UI; add Pagefind postbuild command.
- [ ] Run tests and a fixture production build; commit `feat(site): add static discovery features`.

### Task 7: Gate and Package the Container Build

**Files:** Create `site/scripts/inspect-output.mjs`, `site/tests/output-gate.test.ts`, `site/Dockerfile`, `site/.dockerignore`, `site/Makefile`, modify root `README.md`.

**Interfaces:** `node scripts/inspect-output.mjs dist fixture` verifies required HTML/assets/search/XML/release.json/备案 and rejects SSR artifacts; Docker emits `/workspace/site/dist` under Node 22.20.0.

- [ ] Test inspector failures for missing routes, empty备案, missing article/search/release metadata, and any server manifest.
- [ ] Implement inspector and exact Docker/Make targets `test`, `build`, `verify`, `container-build`.
- [ ] Run `docker build`, build fixture in the image, copy/inspect `dist`, and confirm container `node --version` is `v22.20.0`.
- [ ] Run all Site tests/type checks/build and `git diff --check`; commit `build(site): package verified static output`.

### Task 8: Capture Responsive Visual Regression Baselines

**Files:** Create `site/playwright.config.ts`, `site/tests/visual/site.visual.spec.ts`, `site/tests/visual/__screenshots__/*`, modify `site/package.json`.

**Interfaces:** Playwright serves fixture `dist` and captures home/article at 1440×1000 and 390×844 in dark/light modes.

- [ ] Add assertions for overflow, footer visibility, TOC breakpoints, code scrolling, focus state, and theme persistence before screenshots.
- [ ] Generate reviewed deterministic baselines and verify repeat run has zero pixel diff.
- [ ] Run all Site gates and commit `test(site): lock responsive visual baselines`.

## Phase Completion Gate

- `docker build` confirms Node `22.20.0`, frozen install, tests, Astro check, static build, Pagefind, and output inspection.
- Fixture output contains every required route, metadata artifact, `release.json`, and visible备案; no SSR/server manifest or runtime Markdown parser exists.
- Desktop/mobile light/dark visual baselines pass.

