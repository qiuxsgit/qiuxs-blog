#!/usr/bin/env bash
set -Eeuo pipefail
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
# shellcheck source=lib.sh
source "$script_dir/lib.sh"
[[ $# -eq 5 ]] || die 'usage: deploy-static.sh HOST ROOT RELEASE SOURCE RETAIN'
host=$1; root=$2; release=$3; source_dir=$4; retain=$5; require_json=${DEPLOY_REQUIRE_RELEASE_JSON:-0}
require_root "$root"; require_token "$release"; [[ "$retain" =~ ^[0-9]+$ ]] || die 'invalid retention'; [[ -d "$source_dir" ]] || die 'source directory missing'
stage="$root/.staging/${release}.${BUILD_TAG:-local}.$$"
target="$root/releases/$release"
qroot=$(quote_sh "$root"); qstage=$(quote_sh "$stage"); qtarget=$(quote_sh "$target"); qrelease=$(quote_sh "$release")
remote_sh "$host" "set -eu; mkdir -p $qroot/releases $qroot/.staging; test ! -e $qtarget; rm -rf $qstage; mkdir $qstage"
cleanup() { ssh -o BatchMode=yes "$host" -- rm -rf -- "$stage" 2>/dev/null || true; }
trap cleanup EXIT
rsync -az --delete --exclude='.DS_Store' "$source_dir/" "$host:$stage/"
remote_sh "$host" "set -eu; test -f $qstage/index.html; if [ \"$require_json\" = 1 ]; then test -s $qstage/release.json; fi; mv -- $qstage $qtarget; ln -sfn -- $qrelease $qroot/.current-new; mv -Tf -- $qroot/.current-new $qroot/current; count=0; find $qroot/releases -mindepth 1 -maxdepth 1 -type d -printf '%T@ %p\\n' | sort -nr | cut -d' ' -f2- | while IFS= read -r candidate; do base=\${candidate##*/}; case \"\$base\" in [1-9][0-9]*|[1-9][0-9]*[-._]* ) ;; * ) continue ;; esac; count=\$((count+1)); [ \"\$count\" -le $retain ] || rm -rf -- \"\$candidate\"; done"
trap - EXIT
printf 'deployed %s:%s\n' "$host" "$target"
