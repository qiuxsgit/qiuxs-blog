#!/usr/bin/env bash
set -Eeuo pipefail
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
if "$root/deploy/scripts/render-nginx.sh" bad "$tmp/out" >/dev/null 2>&1; then echo 'malformed upstream accepted' >&2; exit 1; fi
"$root/deploy/scripts/render-nginx.sh" 127.0.0.1:8080 "$tmp/out"
test -f "$tmp/out/blog-admin.conf"; test -f "$tmp/out/qiuxs.com.conf"
rg -q 'root /web/deploy/blog-admin/current' "$tmp/out/blog-admin.conf"
rg -q 'root /web/deploy/blog-site/current' "$tmp/out/qiuxs.com.conf"
rg -q 'location \^~ /api/' "$tmp/out/blog-admin.conf"; rg -q 'location \^~ /api/ \{ return 404' "$tmp/out/qiuxs.com.conf"
if awk '/location \^~ \/api\//,/}/' "$tmp/out/qiuxs.com.conf" | rg -q proxy_pass; then exit 1; fi
rg -q 'location \^~ /img/proxy/' "$tmp/out/qiuxs.com.conf"; ! rg -q 'location \^~ /img/proxy/' "$tmp/out/blog-admin.conf"
! rg -q 'BLOG_SERVICE_UPSTREAM' "$tmp/out"; echo 'nginx contract passed'
