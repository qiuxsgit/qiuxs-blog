#!/usr/bin/env bash
set -Eeuo pipefail

die() { printf 'deploy: %s\n' "$*" >&2; exit 1; }
require_token() { [[ "$1" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$ ]] || die "invalid release id"; }
require_root() {
  case "$1" in
    /web/deploy/blog|/web/deploy/blog-admin|/web/deploy/blog-site) : ;;
    *) die "unapproved deployment root: $1";;
  esac
}
remote() { local host=$1; shift; ssh -o BatchMode=yes "$host" -- "$@"; }
remote_sh() { local host=$1; local command=$2; ssh -o BatchMode=yes "$host" -- sh -c "$command"; }
quote_sh() { printf "'%s'" "${1//'/\'\\\'\'}"; }
