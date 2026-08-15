# Blog deployment runbook

The repository contains three independent Jenkins jobs. They build immutable releases and switch a
`current` symlink only after transfer and inspection succeed. A failed build or transfer leaves the old
release serving traffic. Task 7's browser/Playwright journey is intentionally not part of this project;
verification uses pure tests and shell/configuration contracts, while UI acceptance is manual.

## Fixed targets

| Job | SSH target | Release root |
|---|---|---|
| Service | `root@blogweb1` | `/web/deploy/blog` |
| Admin | `root@ngx1` | `/web/deploy/blog-admin` |
| Site | `root@ngx1` | `/web/deploy/blog-site` |

On first install, create the roots, `releases`, `shared`, `scripts`, `run`, and `logs`, and an
unprivileged `blog` user. Copy `deploy/env/blog.env.example` to
`/web/deploy/blog/shared/blog.env`, fill it out, and let the Jenkins service job install the binary
and `scripts/blog-service.sh`. The service is managed by that control script with `nohup` and a PID
file; no systemd unit is installed.
Run the SQL files under `service/sqls/develop/` manually in their documented order before starting the
Service. Configure the initial administrator through the Service's bootstrap flow.

Install the two generated Nginx files with:

```sh
deploy/scripts/render-nginx.sh 127.0.0.1:8080 /tmp/blog-nginx
cp /tmp/blog-nginx/*.conf /etc/nginx/conf.d/
nginx -t && systemctl reload nginx
```

Configure TLS before public launch. The Admin domain is `blog-admin.qiuxs.com`; the Site domain is
`qiuxs.com` (and `www.qiuxs.com`). The Admin proxies `/api/`; the public domain proxies only
`/img/proxy/`. Dotfiles are denied and static assets are immutable-cached while HTML is no-cache.

## Jenkins jobs

Create jobs from `deploy/jenkins/Jenkinsfile.service`, `.admin`, and `.site`. Service builds with Go
`go1.25.7`; Admin and Site require Node `v20.19.4`. Site parameters are positive numeric `RELEASE_ID`
and `PUBLISH_JOB_ID`; both IDs are preserved in every signed callback. Configure the callback URL,
HMAC secret, service health URL, and SSH credentials in Jenkins. Never copy `.env` or tokens into an
artifact.

## Rollback and retention

List releases on the target and atomically point `current` to a known-good directory:

```sh
ssh root@ngx1 'ln -sfn 123-known-good /web/deploy/blog-site/.rollback-new && mv -Tf /web/deploy/blog-site/.rollback-new /web/deploy/blog-site/current'
ssh root@blogweb1 'ln -sfn 123-known-good /web/deploy/blog/.rollback-new && mv -Tf /web/deploy/blog/.rollback-new /web/deploy/blog/current && /web/deploy/blog/scripts/blog-service.sh restart'
```

The scripts retain the newest five owned releases. Remove only clearly identified old release
directories; never remove `shared`, `current`, or a staging directory belonging to an active build.

## Offline checks

From the repository root run `make test-deploy`. Application checks are `make test-service`,
`make test-admin`, and `make test-site`; `make verify` combines them. These checks use temporary local
directories and fake/argument validation only. They do not connect to Jenkins, MySQL, Redis, GFS, OSS,
`blogweb1`, or `ngx1`. A real deployment and smoke test is an explicit operator-triggered Jenkins action.
