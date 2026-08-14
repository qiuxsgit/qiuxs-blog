#!/usr/bin/env bash
set -Eeuo pipefail
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
mkdir "$tmp/source"; printf '<!doctype html>\n' > "$tmp/source/index.html"
if "$root/deploy/scripts/deploy-static.sh" root@ngx1 /tmp/unsafe x "$tmp/source" 2>/dev/null; then exit 1; fi
if "$root/deploy/scripts/deploy-static.sh" root@ngx1 /web/deploy/blog-site '../escape' "$tmp/source" 5 2>/dev/null; then exit 1; fi
grep -q 'X-Jenkins-Signature' "$root/deploy/scripts/site-callback.sh"
grep -q 'publishJobId' "$root/deploy/scripts/site-callback.sh"
! grep -q 'X-Blog-Signature' "$root/deploy/scripts/site-callback.sh"
echo 'deployment argument gate passed'
