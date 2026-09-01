#!/bin/bash
set -euo pipefail

PROM_ADDR="${PROM_ADDR:-http://prometheus:9090}"
NAMESPACE="${NAMESPACE:-slo-rollout}"
IMAGE_TAG="${IMAGE_TAG:?IMAGE_TAG is required}"

query_value() {
  local query="$1"
  local raw
  raw="$(curl -fsG --data-urlencode "query=${query}" "${PROM_ADDR}/api/v1/query" 2>/dev/null || true)"
  if [ -z "${raw}" ]; then
    echo "unknown"
    return 0
  fi

  echo "${raw}" | jq -r '
    if .status != "success" then "unknown"
    elif (.data.result | length) == 0 then "0"
    else (.data.result[0].value[1] // "unknown")
    end
  ' 2>/dev/null || echo "unknown"
}

REQUEST_COUNT_1M="$(query_value "(sum(increase(demo_http_requests_total{version=\"${IMAGE_TAG}\"}[1m])) or on() vector(0))")"
ERROR_RATE_PERCENT="$(query_value "(((sum(rate(demo_http_requests_total{namespace=\"${NAMESPACE}\",version=\"${IMAGE_TAG}\",status=~\"5..\"}[1m])) or vector(0)) / clamp_min((sum(rate(demo_http_requests_total{version=\"${IMAGE_TAG}\"}[1m])) or vector(0)), 0.001))*100)")"
P95_LATENCY_SECONDS="$(query_value "(histogram_quantile(0.95, sum(rate(demo_http_request_duration_seconds_bucket{version=\"${IMAGE_TAG}\"}[1m])) by (le)) or on() vector(0))")"

echo "OBS_REQUEST_COUNT_1M=${REQUEST_COUNT_1M}"
echo "OBS_ERROR_RATE_PERCENT=${ERROR_RATE_PERCENT}"
echo "OBS_P95_LATENCY_SECONDS=${P95_LATENCY_SECONDS}"
