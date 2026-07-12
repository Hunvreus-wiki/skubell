#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "Usage: $0 <host:port>" >&2
  exit 1
fi

target="$1"
api_url="http://${target}/api.php"

timeout_seconds="${WAIT_TIMEOUT_SECONDS:-180}"
start_time="$(date +%s)"

echo "Waiting for MediaWiki API at ${api_url} ..."
while true; do
  if response="$(curl -fsS --max-time 5 "${api_url}?action=query&meta=siteinfo&format=json" 2>/dev/null)"; then
    if [[ "${response}" == *'"query"'* ]]; then
      echo "Wiki is ready at ${api_url}"
      exit 0
    fi
  fi

  now="$(date +%s)"
  if (( now - start_time >= timeout_seconds )); then
    echo "Timed out waiting for ${api_url} after ${timeout_seconds}s" >&2
    exit 1
  fi

  sleep 2
done
