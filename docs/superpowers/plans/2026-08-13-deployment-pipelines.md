# Jenkins, Nginx, and Deployment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver production-shaped Jenkins pipelines, atomic deployment scripts, Nginx routing, Service process management, and automated end-to-end gates for all three applications.

**Architecture:** Three Jenkinsfiles build independently and deploy immutable releases over preconfigured SSH aliases. Shared POSIX scripts stage via rsync, validate remotely, atomically rename and switch `current`, retaining the previous release on every failure. Nginx serves Admin and Site static `current` links and proxies only scoped Service paths; secrets and the cross-host Service upstream are Jenkins-managed inputs.

**Tech Stack:** Jenkins Declarative Pipeline, Bash/POSIX shell, Docker Node 22.20.0, host Node 20.19.4, host Go 1.25.7, rsync, OpenSSH, Nginx, systemd, Playwright.

## Global Constraints

- Service deploys through `root@blogweb1` to `/web/deploy/blog`.
- Admin deploys through `root@ngx1` to `/web/deploy/blog-admin`.
- Site deploys through `root@ngx1` to `/web/deploy/blog-site`.
- SSH keys, Service `.env`, Jenkins API token, bundle token, callback secret, and `BLOG_SERVICE_UPSTREAM` live in Jenkins/server configuration, never Git.
- Admin host build must report Node `v20.19.4`; Service host build must report Go `go1.25.7`; Site builds only in Node `22.20.0` Docker.
- Static deployments use `releases/<immutable-id>` plus atomic `current` symlink; failed staging never changes `current`.
- Site publication receives both Release and Publish Job IDs and includes both in every signed `building`, `deploying`, and retrying final `success`/`failed` callback.
- Automation tests use temporary local directories and fake ssh/rsync/curl; they never connect to `blogweb1`, `ngx1`, Jenkins, MySQL, Redis, GFS, or OSS.

---

## Planned File Map

- `deploy/jenkins/Jenkinsfile.service`, `.admin`, `.site`: independent jobs.
- `deploy/scripts/deploy-service.sh`, `deploy-static.sh`, `site-callback.sh`, `render-nginx.sh`, `smoke.sh`: audited deployment primitives.
- `deploy/systemd/qiuxs-blog.service`: process unit reading external environment.
- `deploy/nginx/blog-admin.conf.template`, `qiuxs.com.conf.template`: scoped routing templates.
- `deploy/tests/*`: shell behavior and pipeline contract tests.
- `deploy/README.md`: credentials, first install, jobs, rollback, and smoke runbook.
- `Jenkinsfile`: optional multibranch dispatcher linking the three jobs without coupling content publish to application builds.

### Task 1: Define Testable Atomic Deployment Primitives

**Files:** Create `deploy/scripts/lib.sh`, `deploy/scripts/deploy-static.sh`, `deploy/scripts/deploy-service.sh`, `deploy/tests/deploy_scripts.bats`, `deploy/tests/helpers/*`.

**Interfaces:** `deploy-static.sh HOST ROOT RELEASE SOURCE RETAIN`; `deploy-service.sh HOST ROOT RELEASE BINARY UNIT RETAIN`; both validate tokens/absolute approved roots, stage, verify, rename, atomically relink, and prune only older owned releases.

- [ ] Write fake ssh/rsync tests for success, transfer/check failure, existing immutable release, symlink atomicity, quoting/injection rejection, and retaining previous/current releases.
- [ ] Run Bats and observe missing-script failures.
- [ ] Implement scripts with strict mode, explicit allowlisted roots, same-filesystem temporary links, remote `mv`/`ln -sfn`/`mv -T`, and traps that remove staging only.
- [ ] Run focused tests and ShellCheck; commit `feat(deploy): add atomic release primitives`.

### Task 2: Package Service Deployment and Process Ownership

**Files:** Create `deploy/systemd/qiuxs-blog.service`, `deploy/jenkins/Jenkinsfile.service`, `deploy/tests/service_pipeline_test.go`, modify `service/Makefile`, `service/README.md`.

**Interfaces:** Pipeline verifies Go 1.25.7, runs full Service gates, builds static Linux amd64 binary, deploys to `root@blogweb1:/web/deploy/blog`, preserves `/web/deploy/blog/shared/blog.env`, restarts `qiuxs-blog.service`, then checks live/ready.

- [ ] Add static pipeline tests asserting exact version gate, test/race/vet/generate/build order, fixed host/root, no secret copying, systemd hardening, health rollback behavior, and retention.
- [ ] Observe failures, implement Jenkinsfile and unit with `EnvironmentFile=/web/deploy/blog/shared/blog.env`, `ExecStart=/web/deploy/blog/current/blog-service`, restart limits, unprivileged runtime user, filesystem/network hardening compatible with outbound MySQL/Redis/Jenkins/GFS.
- [ ] Run Go pipeline tests plus a local fake deployment; commit `build(service): add jenkins deployment pipeline`.

### Task 3: Package Admin Static Deployment

**Files:** Create `deploy/jenkins/Jenkinsfile.admin`, `deploy/tests/admin_pipeline.test.ts`, modify `admin/package.json`, `admin/README.md`.

**Interfaces:** Pipeline verifies Node v20.19.4, frozen installs, runs type/lint/unit/build/Playwright gates, then atomically deploys `admin/dist` to `root@ngx1:/web/deploy/blog-admin`.

- [ ] Test exact Node gate, credentials-free artifact, fixed host/root, pre-switch HTML/assets inspection, and no Service/Site deployment side effects.
- [ ] Implement pipeline and `admin` CI command; run tests with fake deployment commands.
- [ ] Commit `build(admin): add jenkins deployment pipeline`.

### Task 4: Package the Content-Triggered Site Pipeline

**Files:** Create `deploy/jenkins/Jenkinsfile.site`, `deploy/scripts/site-callback.sh`, `deploy/tests/site_pipeline.test.ts`, modify `site/Dockerfile`, `site/README.md`.

**Interfaces:** Parameters `RELEASE_ID` and `PUBLISH_JOB_ID`; credentials `BLOG_BUILD_TOKEN`, `BLOG_CALLBACK_SECRET`; downloads the Release gzip bundle, validates ETag/checksum, builds/verifies in Docker, deploys to `root@ngx1:/web/deploy/blog-site/releases/<release-id>`, writes `release.json`, and signs callbacks containing the exact `releaseId` and `publishJobId` over canonical JSON + timestamp + nonce.

- [ ] Test RELEASE_ID/PUBLISH_JOB_ID injection rejection, positive numeric validation for both, Bearer redaction, exact stage ordering, callback HMAC fixture covering both IDs, retry/backoff, failure callback in `post`, and that current switches only after remote inspection.
- [ ] Implement pipeline with Docker Node 22.20.0, generated nonce, Jenkins build number, and `building`/`deploying`/`success`/`failed` callbacks that preserve the two input identities unchanged, plus deterministic release metadata.
- [ ] Run static tests and a fully fake pipeline harness; commit `build(site): add release deployment pipeline`.

### Task 5: Configure Nginx for the Two Public Domains

**Files:** Create `deploy/nginx/blog-admin.conf.template`, `deploy/nginx/qiuxs.com.conf.template`, `deploy/scripts/render-nginx.sh`, `deploy/tests/nginx_test.sh`.

**Interfaces:** `render-nginx.sh BLOG_SERVICE_UPSTREAM OUTPUT_DIR` accepts only `host-or-ip:port`, renders configs with Admin root `/web/deploy/blog-admin/current` and Site root `/web/deploy/blog-site/current`.

- [ ] Test missing/malformed upstream rejection, no unresolved template variables, `nginx -t` in a disposable container, SPA `try_files`, immutable asset caching, no-cache HTML, `/api/` proxy only on Admin, `/img/proxy/` proxy only on root domain, forwarded request ID/host/proto, timeouts/body limits, and deny dotfiles.
- [ ] Implement templates and renderer; ensure root domain cannot reach Admin/Internal APIs and Admin cookies never scope to root.
- [ ] Run tests and commit `ops: add blog nginx routing`.

### Task 6: Add Cross-Application CI and Repository Gates

**Files:** Create root `Makefile`, `deploy/scripts/verify-repository.sh`, `deploy/tests/repository_gate_test.sh`, optional root `Jenkinsfile`, modify root `README.md`.

**Interfaces:** Root targets `test-service`, `test-admin`, `test-site`, `test-deploy`, `verify`; dispatcher changes only build affected applications and never uses the application-build job for content publication.

- [ ] Test required directories/contracts/lockfiles, exact toolchain declarations, SQL rules, forbidden secret patterns, SSR absence, and shell syntax.
- [ ] Implement aggregate commands and change-path job dispatch.
- [ ] Run complete repository verification and commit `build: add cross-application verification gates`.

### Task 7: Add Offline Playwright Release Journey

**Files:** Create `e2e/package.json`, `e2e/pnpm-lock.yaml`, `e2e/playwright.config.ts`, `e2e/tests/blog-release.spec.ts`, `e2e/fixtures/fake-service.ts`.

**Interfaces:** Fake Service implements the generated Admin/Internal contract in memory; test covers login, article creation, whole Markdown paste, image policy/direct-upload simulation/register, autosave, preview, version, publish, immutable bundle build, and resulting static article.

- [ ] Write the end-to-end test and observe failure before applications are wired.
- [ ] Implement only the fake boundary and orchestration helpers necessary to run real Admin and real Site builds; do not duplicate application logic.
- [ ] Run desktop Chromium flow and commit `test: cover offline blog release journey`.

### Task 8: Document First Deployment, Operations, and Manual Smoke Test

**Files:** Create `deploy/README.md`, `deploy/env/blog.env.example`, `deploy/jenkins/credentials.md`, modify root `README.md`.

**Interfaces:** Runbook enumerates Jenkins credentials/variables including `BLOG_SERVICE_UPSTREAM`, directory/bootstrap ownership, manual SQL order, admin initialization, three job setup, Nginx rendering/install, TLS prerequisite, callbacks, health checks, log locations, manual rollback, and release retention.

- [ ] Add documentation contract tests that extract every required command/path/credential and reject secrets/example real tokens.
- [ ] Write exact first-install and rollback commands using the fixed SSH aliases and roots; clearly label real-network smoke tests as operator-run and non-automated.
- [ ] Run repository gates and commit `docs: add blog deployment runbook`.

## Phase Completion Gate

- All application and deploy test suites pass without real infrastructure; pipeline fixtures prove exact versions and destinations.
- Nginx templates validate after explicit `BLOG_SERVICE_UPSTREAM` injection and expose only intended paths.
- Fake failure at every transfer/build/check/switch/callback point preserves the previous `current` target.
- The offline browser journey produces and opens the final static article.
- Real deployment remains an operator-triggered Jenkins action because credentials and target machines are external state.
