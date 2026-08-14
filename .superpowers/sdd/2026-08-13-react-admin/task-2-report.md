# Task 2 Report: Generated Types and Sanitized API Boundary

## Status

Complete on approved `main` baseline `d956b0b78df3011b788a9faf587631ef83b4e645` using Node `20.19.4` and npm `10.8.2`.

Commit: `4805866` (`feat(admin): add generated api client`)

## TDD evidence

### Initial RED

After the first `npm run generate:api`, the complete boundary test was added before the adapter, ID guard, query keys, or fixtures existed.

Command:

```text
cd admin
source /Users/qiuxs/.nvm/nvm.sh && nvm use 20.19.4
npm test -- --run src/api/admin-api.test.ts
```

Observed result:

```text
Test Files  1 failed (1)
Tests       no tests
Error: Failed to resolve import "./admin-api" from "src/api/admin-api.test.ts".
```

This was the expected missing-boundary failure.

### Path-ID RED/GREEN sub-cycle

An API-level invalid-path test first failed because `execute` translated the synchronous ID guard into `network_error` (`503`) instead of preserving `invalid_api_response` (`502`). Path IDs were then validated before entering the network exception boundary.

Observed RED:

```text
Tests  1 failed | 9 passed (10)
Expected code/status: invalid_api_response/502
Received code/status: network_error/503
```

Observed GREEN:

```text
Test Files  1 passed (1)
Tests       10 passed (10)
```

### Full test-runner bridge RED/GREEN

`npm test -- --run` exposed an existing gate defect: Vitest's default glob collected `scripts/*.test.mjs`, then Vite/jsdom failed to bundle the Node built-in `node:test`. The minimal bridge limits Vitest discovery to `src/**/*.{test,spec}.{ts,tsx}`; Node-runner tests remain under `node --test`.

Observed RED:

```text
Test Files  2 failed | 1 passed (3)
Error: Cannot bundle Node.js built-in "node:test" imported from "scripts/*.test.mjs".
```

Observed GREEN:

```text
npm run test:run
Test Files  1 passed (1)
Tests       10 passed (10)
```

## 29-operation coverage

The test invokes every `AdminApi` member and asserts its exact method, path, query string, JSON body (or absence), and documented success status:

1. `loginAdmin`
2. `logoutAdmin`
3. `getCurrentAdmin`
4. `listArticles`
5. `createArticle`
6. `getArticle`
7. `saveArticleDraft`
8. `getArticlePreview`
9. `listArticleVersions`
10. `createArticleVersion`
11. `restoreArticleVersion`
12. `trashArticle`
13. `untrashArticle`
14. `listTags`
15. `createTag`
16. `renameTag`
17. `createMediaUploadPolicy`
18. `registerMedia`
19. `getSiteSettings`
20. `putSiteSettings`
21. `getHotlinkSettings`
22. `putHotlinkSettings`
23. `getBuilderConfig`
24. `putBuilderConfig`
25. `testBuilderConfig`
26. `listReleases`
27. `createRelease`
28. `getRelease`
29. `retryRelease`

Coverage also verifies:

- `createArticle`, `createMediaUploadPolicy`, and `testBuilderConfig` send no JSON body.
- All four documented `204` operations resolve `undefined`.
- Only exact documented success statuses/data are accepted.
- A `401 application/problem+json` with `unauthenticated` invokes the callback; other `401` codes do not.
- `builder_conflict` and `future_service_code` remain verbatim strings.
- Invalid input and returned entity IDs are rejected.
- Network failures and invalid responses become fixed client problems without raw bodies, passwords, or tokens.
- Canonical fixtures use generated schema aliases.
- Article and release query-key invalidation hierarchies match the brief.
- The MSW server uses 29 default handlers and throws on unhandled requests.

## Generation reproducibility

Initial generation:

```text
openapi-typescript 7.13.0
../contracts/openapi/admin-v1.yaml -> src/api/generated/admin.ts
```

The command was run a second time after staging the generated file:

```text
npm run generate:api
git diff --exit-code -- src/api/generated/admin.ts
```

Both commands exited `0`; the second generation produced no working-tree diff.

## Verification

```text
npm test -- --run src/api/admin-api.test.ts  # 10/10 passed
npm test -- --run src                       # 10/10 passed
npm run test:run                            # 10/10 passed
npm run typecheck                           # exit 0
node --test scripts/*.test.mjs              # 2/2 passed
npm run build                               # exit 0
git diff --cached --check                   # exit 0
```

## Files

- `admin/src/api/generated/admin.ts`
- `admin/src/api/admin-api.ts`
- `admin/src/api/admin-api.test.ts`
- `admin/src/api/ids.ts`
- `admin/src/api/problem.ts`
- `admin/src/api/query-keys.ts`
- `admin/src/test/fixtures.ts`
- `admin/src/test/handlers.ts`
- `admin/src/test/server.ts`
- `admin/src/test/render.tsx`
- `admin/src/test/setup.ts`
- `admin/vitest.config.ts` (full test-runner bridge)

`admin/package.json` and `admin/package-lock.json` were included in the prescribed staging command but had no Task 2 diff.

## Concerns

No unresolved Task 2 concerns. The Vitest/Node-runner discovery conflict found during full verification is fixed by the scoped include pattern, and both runners are green independently.

## Fix Round 1

Independent review: `.superpowers/sdd/2026-08-13-react-admin/task-2-review.md`.

Commit: `a7ad740` (`fix(admin): harden api boundary contracts`)

### Finding 1: cancellation contract

RED (`npm test -- --run src/api/admin-api.test.ts`): 2 failures out of 11 tests. All 29 recorded request signals remained un-aborted after aborting the caller signal, and a fetch `AbortError` became `503/network_error`.

GREEN: every `AdminApi` member now has a final `signal?: AbortSignal`, every openapi-fetch call receives it, all 29 fetch-level signals follow the caller signal, and `AbortError` is rethrown unchanged. Focused result: 11/11.

### Finding 2: schema-aware int64 boundary

RED: 14 failures out of 26 tests. Nine invalid request cases performed one fetch each; five invalid response fields (`revisionNo`, draft/site `lockVersion`, `fileSize`, and `buildNumber`) incorrectly resolved.

GREEN: generated-schema-aware request validators run before fetch for save/version/restore/media/site/release bodies. Response validators cover every int64 reachable from the 29 Admin operations: all IDs, revision numbers, draft/site lock versions, media file size, and nullable publish-job build numbers, using safe-integer and schema minimum rules. Minimum and nullable edges remain accepted. Focused result: 26/26.

### Finding 3: serializable Problem title and secret redaction

RED: 2 failures out of 28 tests. `ApiProblem` lacked `title`; valid Problem `type`, `requestId`, and `code` fields could serialize the submitted password/token.

GREEN: `ApiProblem.title` is enumerable/serializable. Safe server `title`, `code`, `requestId`, and `type` values are preserved. Only fields containing a non-empty sensitive value from the current login/builder request use fixed fallbacks. Safe unknown codes remain verbatim, and valid Problem tests prove `JSON.stringify(error)` contains no password/token. Focused result: 28/28.

### Finding 4: transport versus parser/response classification

RED: 3 failures out of 32 tests. Malformed success JSON became `503/network_error`, structurally valid data under `application/problem+json` resolved, and null data did not use the fixed generic invalid-response title. A follow-up shape gate produced 3 failures out of 35 tests: application/json responses missing required Admin fields and Problem-shaped MediaUploadPolicy/HotlinkSettings objects incorrectly resolved.

GREEN: the configured fetch is wrapped so only an actual fetch rejection is tagged as `503/network_error`; parser failures become fixed `502/invalid_api_response`, while abort remains unchanged. Data-returning successes require the exact documented status, `application/json`, a non-null object, and the generated schema's required top-level shape before int64 validation. Nested draft/tag/media/job/release structures validated by the boundary also enforce their required keys. Malformed JSON, wrong content type, null/missing data, Problem-shaped success objects, and raw response text cannot escape the boundary. Focused result: 35/35.

### Fix Round 1 verification

```text
Node 20.19.4 npm test -- --run src/api/admin-api.test.ts  # 35/35 passed
Node 20.19.4 npm run test:run                             # 35/35 passed
Node 20.19.4 npm run typecheck                            # exit 0
Node 20.19.4 npm run build                                # exit 0
Node 20.19.4 node --test scripts/*.test.mjs               # 2/2 passed
Node 20.19.4 npm run generate:api                         # exit 0
git diff --exit-code -- src/api/generated/admin.ts        # exit 0
git diff --check                                          # exit 0
```

Fix Round 1 concerns: none unresolved.

## Fix Round 2

Re-review scope: the remaining Important finding in `.superpowers/sdd/2026-08-13-react-admin/task-2-review.md` after Fix Round 1.

Commit message: `fix(admin): validate generated response shapes`

### Strict runtime response shapes

RED: a table-driven boundary suite was added before production changes. The focused run reported `100 failed | 45 passed (145)`; the first failure reproduced the review exactly because `{id: 1, username: 42}` resolved from `getCurrentAdmin()`. Article scalar/enum/nested mutations and the other response families likewise crossed the boundary. One independently identified nullable-positive fixture setup error (an ArticleList body missing its `items` wrapper) was corrected before implementation.

GREEN: response validators now consume `unknown` and use strict record, required-field, string, boolean, enum, safe-integer, array, and nullable helpers. Every documented required field is checked for its primitive type, enum membership, nullability, and nested element/object shape. Additional fields remain ignored. Draft and frozen revision enums are distinct; tag/media references, social links, hotlink entries, publish jobs, builder targets, releases, and every list/result wrapper validate recursively. Response failures always use the fixed `502/invalid_api_response` Problem and never serialize the rejected value.

The final table has invalid cases for Admin; Article summary/detail/draft/preview/revision/version; Tag; Media policy/view; Site settings/social links; Hotlink settings/entries; Builder; Release/PublishJob/builderTarget; Article/Revision/Tag/Release list wrappers; and Version/CreateRelease/RetryRelease result wrappers. It also proves `null` remains accepted only for the schema's nullable article, draft, site, release, and publish-job fields.

### Fix Round 2 verification

```text
Node 20.19.4 npm test -- --run src/api/admin-api.test.ts  # 180/180 passed
Node 20.19.4 npm run test:run                             # 180/180 passed
Node 20.19.4 npm run typecheck                            # exit 0
Node 20.19.4 npm run build                                # exit 0
Node 20.19.4 node --test scripts/*.test.mjs               # 2/2 passed
Node 20.19.4 npm run generate:api                         # exit 0
git diff --exit-code -- src/api/generated/admin.ts        # exit 0
git diff --check                                          # exit 0
```

Fix Round 2 concerns: none unresolved. Generated files and Task 3 files were not changed.
