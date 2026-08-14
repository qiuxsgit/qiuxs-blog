#!/usr/bin/env bash
set -Eeuo pipefail
[[ $# -eq 6 ]] || { echo 'usage: site-callback.sh URL SECRET RELEASE_ID PUBLISH_JOB_ID STATUS BUILD_NUMBER' >&2; exit 1; }
url=$1; secret=$2; release_id=$3; publish_job_id=$4; status=$5; build_id=$6
[[ "$release_id" =~ ^[1-9][0-9]*$ && "$publish_job_id" =~ ^[1-9][0-9]*$ ]] || { echo 'IDs must be positive numeric values' >&2; exit 1; }
[[ "$status" =~ ^(building|deploying|success|failed)$ ]] || { echo 'invalid status' >&2; exit 1; }
command -v curl >/dev/null || { echo 'curl required' >&2; exit 1; }
command -v openssl >/dev/null || { echo 'openssl required' >&2; exit 1; }
timestamp_epoch=$(date +%s); timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ); nonce=$(openssl rand -hex 16)
case "$status" in
  building) stage=build; error_summary=;;
  deploying|success) stage=deploy; error_summary=;;
  failed) stage=deploy; error_summary='Jenkins pipeline failed';;
esac
# Keep this field order and the timestamp/nonce separators identical to the
# service verifier's canonical CallbackPayload signing contract.
payload=$(printf '{"releaseId":%s,"publishJobId":%s,"buildNumber":%s,"stage":"%s","status":"%s","errorSummary":"%s","timestamp":"%s","nonce":"%s"}' "$release_id" "$publish_job_id" "$build_id" "$stage" "$status" "$error_summary" "$timestamp" "$nonce")
signature=$(printf '%s\n%s\n%s' "$timestamp_epoch" "$nonce" "$payload" | openssl dgst -sha256 -hmac "$secret" -binary | od -An -tx1 | tr -d ' \n')
curl --fail-with-body --silent --show-error --retry 3 --retry-delay 2 --connect-timeout 5 --max-time 30 \
  -H 'Content-Type: application/json' -H "X-Jenkins-Signature: sha256=$signature" \
  --data "$payload" "$url"
