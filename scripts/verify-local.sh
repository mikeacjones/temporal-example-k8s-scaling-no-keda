#!/usr/bin/env bash
# Isolated SDK smoke test. It does not assert Cloud backlog/HPA behavior.
set -euo pipefail
cd "$(dirname "$0")/.."
compose=(docker compose -p temporal-hpa-example-check -f docker-compose.yml)
cleanup() { "${compose[@]}" down --volumes; }
trap cleanup EXIT
"${compose[@]}" up --build -d
"${compose[@]}" exec -T temporal temporal --disable-config-file \
  --address 127.0.0.1:7233 worker deployment set-current-version \
  --deployment-name media-demo/media --build-id v1 --yes
"${compose[@]}" run --rm --no-deps worker load -count=8 -interval=50ms -activity-duration=5s
docker run --rm --network temporal-hpa-example-check_default python:3.13-alpine python -c '
import urllib.request
s=urllib.request.urlopen("http://worker:9090/metrics",timeout=10).read().decode()
gauges=[x for x in s.splitlines() if x.startswith("temporal_worker_task_slots_used{") and "ActivityWorker" in x]
assert gauges and any(float(x.rsplit(" ",1)[1]) > 0 for x in gauges), gauges
assert urllib.request.urlopen("http://worker:9090/healthz",timeout=10).status == 200
print("Activity slot gauge becomes nonzero under actual workflow load; readiness is healthy.")
'
sleep 12
"${compose[@]}" exec -T temporal temporal --disable-config-file \
  --address 127.0.0.1:7233 workflow count --query 'ExecutionStatus="Completed"' --output json
