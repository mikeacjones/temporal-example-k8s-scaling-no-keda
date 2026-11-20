# Temporal workers with Prometheus and HPA

This Helm example scales each Temporal worker version independently using Activity slot utilization and Task Queue backlog. It uses Prometheus and Prometheus Adapter with native Kubernetes HPA. KEDA is not installed.

The package includes a small Go worker, a finite load generator, and a two-node kind cluster definition. The chart targets an existing Temporal Cloud namespace. Helm installs resources into a cluster; kind creates the optional local cluster.

## How it works

```text
Worker /metrics ── slot gauges ──┐
                                ├─ Prometheus ─ Prometheus Adapter ─ HPA per version
Temporal Cloud ── backlog ───────┘                                    │
                                                         versioned Deployment
Worker Controller ─ creates versions, HPAs, routing and cleanup ──────┘
```

The Worker Controller renders one HPA from [hpa-template.yaml](chart/templates/hpa-template.yaml) for every running worker version. It injects the version's Deployment target and these metric selectors:

- `temporal_worker_deployment_name`: Kubernetes namespace and WorkerDeployment name joined by `_`.
- `temporal_worker_build_id`: that version's Build ID.
- `temporal_namespace`: the Temporal namespace.

Prometheus adds the same identity labels to the scraped worker metrics from Kubernetes pod metadata. The Cloud scrape requests the version labels explicitly. [prometheus-config.yaml](chart/templates/prometheus-config.yaml) contains both the scrape jobs and the recording rule.

| Metric | HPA target | Default | Effect |
| --- | --- | --- | --- |
| Activity slots used / total slots | `Value` | `750m` = 75% | Scales from worker occupancy, including when backlog is zero |
| Pending Activity tasks | `AverageValue` | 4 tasks per replica | Adds capacity when tasks wait in the queue |

HPA calculates both replica recommendations and takes the larger one. For example, four replicas at 90% utilization with a 75% target recommend five replicas; a backlog of 24 with four tasks per replica recommends six. The result is six, subject to the configured limits and stabilization.

## Run it

You need Docker, kind, Helm 3 and kubectl. Allocate about 4 CPUs and 6 GB of memory to Docker for the local cluster. No host Go installation is needed. Use a dedicated Temporal Cloud namespace for the example.

After cloning, download the locked chart dependencies:

```sh
make deps
```

1. Create the optional local cluster and select its isolated kubeconfig:

   ```sh
   make cluster
   export KUBECONFIG="$PWD/.run/kubeconfig"
   make bootstrap
   ```

   The bootstrap script installs cert-manager, the Worker Controller CRDs, and the controller as separate releases. It always passes an explicit Kubernetes context. For an existing cluster, omit `make cluster` and use `make bootstrap KUBE_CONTEXT=your-context` only if those components are not already installed.

2. Copy [values.example.yaml](values.example.yaml) to `values.local.yaml`. Set your Cloud address and namespace. Keep the example Task Queue name `media-processing`, or use another name containing lowercase letters, digits and hyphens.

   Create the two Secrets from local files containing the raw API keys:

   ```sh
   kubectl --context kind-temporal-worker-hpa create namespace media-demo
   kubectl --context kind-temporal-worker-hpa -n media-demo create secret generic temporal-api-key \
     --from-file=api-key=/path/to/namespace-api-key.txt
   kubectl --context kind-temporal-worker-hpa -n media-demo create secret generic temporal-cloud-metrics \
     --from-file=api-key=/path/to/metrics-api-key.txt
   ```

   The namespace API key must allow the worker and controller to run workflows and manage Worker Deployment Versions. The metrics API key is a separate Observability credential for `https://metrics.temporal.io/v1/metrics`. Both Secrets stay outside Helm values and the package.

3. Build the sample worker and load it into kind:

   ```sh
   make image TAG=v1
   kind load docker-image temporal-hpa-worker:v1 --name temporal-worker-hpa
   ```

   For an existing cluster, push the image to your registry and change `worker.image.repository`.

4. Install the chart:

   ```sh
   helm upgrade --install media ./chart \
     --kube-context kind-temporal-worker-hpa \
     --namespace media-demo -f values.local.yaml --wait --timeout 5m
   kubectl --context kind-temporal-worker-hpa -n media-demo get workerdeployments,workerresourcetemplates,hpa
   ```

   Dependencies are pinned by `Chart.lock`; their downloaded archives are ignored by Git. Run `make deps` after cloning. Wait until the worker version becomes Current and its HPA has metric values before starting load. The first version still needs to register and run its rollout gate.

5. Submit one finite batch and watch:

   ```sh
   make load
   kubectl --context kind-temporal-worker-hpa -n media-demo get deployments,hpa -w
   ```

   The batch submits 60 workflows, spaced 500 ms apart. Each performs a simulated 30-second Activity. It stops submitting after the batch. Use [RUNBOOK.md](RUNBOOK.md) for a version rollout, Prometheus queries and troubleshooting.

## Change the version

Build and load a new immutable tag, then upgrade:

```sh
make image TAG=v2
kind load docker-image temporal-hpa-worker:v2 --name temporal-worker-hpa
helm upgrade media ./chart --kube-context kind-temporal-worker-hpa \
  --namespace media-demo -f values.local.yaml --set worker.image.tag=v2
```

The sample code stays the same so this demonstrates deployment mechanics without a workflow code change. The changed image tag produces a new Build ID. The controller runs the rollout gate, ramps new work through 10% and 50%, then promotes the new version. Existing Pinned workflows finish on their original version. Each version has a separate HPA.

## Use your own worker

Replace the image, readiness probe and port configuration in [worker.yaml](chart/templates/worker.yaml) to match your app. Your worker needs to:

- Read the controller-injected connection settings, `TEMPORAL_DEPLOYMENT_NAME` and `TEMPORAL_WORKER_BUILD_ID`.
- Enable Worker Versioning and use the injected Deployment name and Build ID.
- Export the SDK's `temporal_worker_task_slots_used` and `temporal_worker_task_slots_available` gauges with `task_queue` and `worker_type` labels.
- Register your gate workflow, or remove the example `rollout.gate` configuration.
- Handle Activity cancellation and retries appropriately.

[main.go](sample-worker/main.go) shows those settings. The chart deliberately omits `spec.deployment.replicas`; the HPA owns replica counts. Do not add a fixed replicas value alongside it.

## Use an existing monitoring stack

Set `prometheus.enabled=false` and `adapter.enabled=false` when your cluster already provides these services. Merge the two scrape jobs and recording rule from the rendered Prometheus ConfigMap into your existing Prometheus configuration. Merge `adapter.rules.external` from [values.yaml](chart/values.yaml) into the existing Prometheus Adapter configuration.

An existing metrics adapter can also serve the two metrics if it honors the HPA's label selectors. Keep the namespace, deployment name and Build ID filters intact. A cluster has one provider for `external.metrics.k8s.io`; the bootstrap script checks for an existing provider.

## Settings and limits

All tuning is in [chart/values.yaml](chart/values.yaml). The default is 1–10 replicas **per active version**, with a five-minute downscale stabilization window and at most one pod removed per minute. Two active versions can therefore use up to 20 worker pods during a rollout.

Cloud backlog metrics arrive on the Cloud metrics cadence and can lag by minutes. Slot metrics come directly from the pods. Missing metrics are a configuration problem to investigate; the example does not turn a missing series into a zero.

This HPA setup keeps at least one replica for each active version. HPA scaling can still remove a pod running an Activity; slot utilization does not provide pod-level eviction protection. The sample has heartbeats, a 45-second worker shutdown period and a 60-second pod grace period. Real media processing Activities need an appropriate retry/checkpoint strategy.

The bundled Prometheus uses ephemeral storage and two-hour retention for this example. The Task Queue name restriction keeps its metric selector labels valid without extra normalization.

## Verify and clean up

`make test` renders and lints the Helm chart, checks the scaling configuration, builds/tests the Go worker in Docker, and runs Prometheus rule tests. The validation utilities run in a pinned Python container; no host Python packages are needed. Structural resource validation also checks the pinned controller CRDs and a generated HPA against Kubernetes schemas. It does not execute admission webhooks.

Stop submitting load and let the finite workflows finish before removing the workers. Delete the local cluster with `make down`. On an existing cluster, uninstall the `media` release after workflows have finished; retain shared controller and monitoring installations.

See [VALIDATION.md](VALIDATION.md) for the checks actually performed on this package.

## Versions and references

| Component | Pin |
| --- | --- |
| Worker Controller chart and CRD chart | 0.32.0 |
| Worker Controller image | 1.14.0 |
| Prometheus chart | 29.48.0 |
| Prometheus Adapter chart | 5.4.2 |
| cert-manager | v1.21.3 |
| kind node | v1.35.8, pinned digest |
| Go / Temporal Go SDK | 1.26.10 / 1.46.0 |

The controller source at v1.14.0 contains chart 0.32.0 with a 1.13.0 default image; the supplied controller values explicitly select 1.14.0.

References: [Worker Controller](https://docs.temporal.io/production-deployment/worker-deployments/kubernetes-controller), [resource templates at the pinned source revision](https://github.com/temporalio/temporal-worker-controller/blob/56318aa53962a8f7127bf07e9e3d31c855cb593b/docs/worker-resource-templates.md), [scaling recommendations](https://github.com/temporalio/temporal-worker-controller/blob/56318aa53962a8f7127bf07e9e3d31c855cb593b/docs/scaling-recommendations.md).

The example source is MIT licensed. Bundled dependency charts retain their upstream licenses.
