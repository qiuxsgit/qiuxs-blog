#!/usr/bin/env bash
set -Eeuo pipefail
root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
for path in service admin site deploy/nginx deploy/scripts deploy/jenkins; do test -d "$root/$path"; done
test -f "$root/admin/package-lock.json"; test -f "$root/site/package-lock.json"
! rg -n 'BEGIN (RSA|OPENSSH) PRIVATE KEY|AKID[0-9A-Z]+|JENKINS_TOKEN=' "$root" --glob '!*.md' --glob '!.git/**' >/dev/null
bash -n "$root"/deploy/scripts/*.sh
echo 'repository gate passed'
