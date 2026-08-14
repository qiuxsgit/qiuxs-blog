#!/usr/bin/env bash
set -Eeuo pipefail
[[ $# -eq 6 ]] || { echo 'usage: site-callback.sh URL SECRET RELEASE_ID PUBLISH_JOB_ID STATUS BUILD_ID' >&2; exit 1; }
url=$1; secret=$2; release_id=$3; publish_job_id=$4; status=$5; build_id=$6
[[ "$release_id" =~ ^[1-9][0-9]*$ && "$publish_job_id" =~ ^[1-9][0-9]*$ ]] || { echo 'IDs must be positive numeric values' >&2; exit 1; }
[[ "$status" =~ ^(building|deploying|success|failed)$ ]] || { echo 'invalid status' >&2; exit 1; }
command -v curl >/dev/null || { echo 'curl required' >&2; exit 1; }
command -v openssl >/dev/null || { echo 'openssl required' >&2; exit 1; }
timestamp=$(date +%s); nonce=$(openssl rand -hex 16)
payload=$(printf '{"buildId":"%s","nonce":"%s","publishJobId":%s,"releaseId":%s,"status":"%s","timestamp":%s}' "$build_id" "$nonce" "$publish_job_id" "$release_id" "$status" "$timestamp")
signature=$(printf '%s' "$timestamp.$nonce.$payload" | openssl dgst -sha256 -hmac "$secret" -binary | od -An -tx1 | tr -d ' \n')
curl --fail-with-body --silent --show-error --retry 3 --retry-delay 2 --connect-timeout 5 --max-time 30 \
  -H 'Content-Type: application/json' -H "X-Blog-Timestamp: $timestamp" -H "X-Blog-Nonce: $nonce" -H "X-Blog-Signature: sha256=$signature" \
  --data "$payload" "$url"
