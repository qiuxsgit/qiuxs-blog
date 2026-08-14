#!/usr/bin/env bash
set -Eeuo pipefail
[[ $# -eq 2 ]] || { echo 'usage: render-nginx.sh HOST:PORT OUTPUT_DIR' >&2; exit 1; }
upstream=$1; out=$2
[[ "$upstream" =~ ^([A-Za-z0-9][A-Za-z0-9.-]*|[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+):[1-9][0-9]{0,4}$ ]] || { echo 'invalid BLOG_SERVICE_UPSTREAM' >&2; exit 1; }
[[ "$upstream" != *'..'* ]] || { echo 'invalid BLOG_SERVICE_UPSTREAM' >&2; exit 1; }
mkdir -p "$out"
template_dir=$(CDPATH= cd -- "$(dirname -- "$0")/../nginx" && pwd)
for name in blog-admin qiuxs.com; do
  BLOG_SERVICE_UPSTREAM=$upstream envsubst '$BLOG_SERVICE_UPSTREAM' < "$template_dir/$name.conf.template" > "$out/$name.conf"
  ! rg -q 'BLOG_SERVICE_UPSTREAM' "$out/$name.conf" || { echo "unresolved upstream in $name" >&2; exit 1; }
done
