#!/usr/bin/env bash
set -Eeuo pipefail
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
source "$script_dir/lib.sh"
[[ $# -eq 6 ]] || die 'usage: deploy-service.sh HOST ROOT RELEASE BINARY UNIT RETAIN'
host=$1; root=$2; release=$3; binary=$4; unit=$5; retain=$6
require_root "$root"; require_token "$release"; [[ "$retain" =~ ^[0-9]+$ ]] || die 'invalid retention'; [[ -f "$binary" ]] || die 'binary missing'; [[ "$unit" =~ ^[A-Za-z0-9_.@-]+\.service$ ]] || die 'invalid unit'
stage="$root/.staging/${release}.${BUILD_TAG:-local}.$$"; target="$root/releases/$release"
qroot=$(quote_sh "$root"); qstage=$(quote_sh "$stage"); qtarget=$(quote_sh "$target"); qrelease=$(quote_sh "$release"); qunit=$(quote_sh "$unit")
remote_sh "$host" "set -eu; mkdir -p $qroot/releases $qroot/.staging $qroot/shared; test ! -e $qtarget; rm -rf $qstage; mkdir $qstage"
cleanup() { ssh -o BatchMode=yes "$host" -- rm -rf -- "$stage" 2>/dev/null || true; }
trap cleanup EXIT
rsync -az "$binary" "$host:$stage/blog-service"
remote_sh "$host" "set -eu; test -x $qstage/blog-service || chmod 0755 $qstage/blog-service; test -s $qstage/blog-service; previous=; test ! -L $qroot/current || previous=\$(readlink $qroot/current); mv -- $qstage $qtarget; ln -sfn -- $qrelease $qroot/.current-new; mv -Tf -- $qroot/.current-new $qroot/current; if ! systemctl restart $qunit || ! systemctl is-active --quiet $qunit; then if test -n \"\$previous\"; then ln -sfn -- \"\$previous\" $qroot/.rollback-new; mv -Tf -- $qroot/.rollback-new $qroot/current; systemctl restart $qunit || true; fi; exit 1; fi; count=0; find $qroot/releases -mindepth 1 -maxdepth 1 -type d -printf '%T@ %p\\n' | sort -nr | cut -d' ' -f2- | while IFS= read -r candidate; do base=\${candidate##*/}; case \"\$base\" in [1-9][0-9]*|[1-9][0-9]*[-._]* ) ;; * ) continue ;; esac; count=\$((count+1)); [ \"\$count\" -le $retain ] || rm -rf -- \"\$candidate\"; done"
trap - EXIT
printf 'deployed %s:%s and restarted %s\n' "$host" "$target" "$unit"
