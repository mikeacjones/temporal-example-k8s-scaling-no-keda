# Validation

Validated on November 20, 2026.

| Check | Result |
| --- | --- |
| Helm strict lint and rendering with bundled dependencies | Passed |
| WorkerDeployment, Connection and WorkerResourceTemplate structural schemas at the pinned controller source revision | Passed |
| Generated per-version HPA against Kubernetes v1.35 schema | Passed |
| Rejection of zero minimum replicas and minimum greater than maximum | Passed |
| Prometheus scrape configuration and recording-rule syntax with promtool v3.16.0 | Passed |
| Slot ratio: v1 at 75%, idle v2 remains separate | Passed |
| Backlog: priorities aggregate within a version; v1/v2 stay separate | Passed |
| Go sample build and workflow/rollout-gate tests in Docker | Passed |
| Local versioned worker connects and becomes Current | Passed |
| Actual SDK gauges become nonzero under local workflow load; readiness returns 200 | Passed |
| Finite local load: eight workflows complete | Passed |
| Shell syntax and README/runbook writing checks | Passed |

The local test used the built runtime image and Temporal CLI dev server 1.10.1. Its containers and network were removed after the test.

Not run: installation into a Kubernetes cluster, Kubernetes admission-webhook/CEL validation, live Temporal Cloud metrics ingestion, HPA replica changes, or a two-version rollout against Temporal Cloud. Those checks require a target cluster and a Cloud namespace plus its namespace and metrics credentials. No Cloud credentials are included.

Reproduce the static, schema, SDK and Prometheus checks with `make test`. Reproduce the isolated local SDK check with `bash scripts/verify-local.sh`. The local dev server does not expose the Cloud backlog feed, so it cannot prove Cloud HPA behavior.
