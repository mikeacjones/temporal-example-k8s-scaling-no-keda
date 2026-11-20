# Show versioned autoscaling

Use the README to install the chart in a dedicated namespace. This is a standalone infrastructure example; it has no hosted catalog deployment.

## Before starting

Confirm the first WorkerDeployment version is Current, its gate succeeded, and the HPA reports both metrics. Allow several minutes for the first Cloud backlog series and adapter discovery. A zero backlog is useful data; an unknown metric needs investigation.

```sh
kubectl --context kind-temporal-worker-hpa -n media-demo get workerdeployments,workerresourcetemplates
kubectl --context kind-temporal-worker-hpa -n media-demo describe hpa
```

## 1. Show the metric sources

Forward the Prometheus service in a separate terminal:

```sh
kubectl --context kind-temporal-worker-hpa -n media-demo port-forward svc/media-prometheus-server 9090:80
```

Open `http://localhost:9090`. On the Targets page, confirm worker targets and the Cloud target are healthy. Query:

```promql
temporal_slot_utilization{worker_type="ActivityWorker",task_queue="media-processing"}
```

```promql
temporal_cloud_v1_approximate_backlog_count{temporal_task_queue="media-processing",task_type="Activity"}
```

Show `temporal_worker_build_id` on each series. Slots come from SDK gauges on every worker pod. Backlog comes from the Cloud endpoint and is not invented by the worker.

## 2. Show scale up

```sh
make load
kubectl --context kind-temporal-worker-hpa -n media-demo get deployments,hpa -w
```

Open the worker Deployment's HPA description to see the current values and desired replica count. Slot occupancy normally reacts first. Backlog provides another capacity recommendation after the Cloud metrics pipeline catches up.

Show [the HPA template](chart/templates/hpa-template.yaml). The utilization ratio uses `Value: 750m`. Backlog uses `AverageValue: 4`. HPA chooses whichever asks for more replicas.

## 3. Add another version

For a longer observation window, set `load.count=120` and `load.activityDuration=2m` in `values.local.yaml`, then upgrade. Start a batch before the rollout so v1 has work to finish.

Build/load v2 and run the upgrade from the README. Keep watching Deployments and HPAs. The new version gets its own Deployment, HPA and Build ID. The controller gates and gradually promotes it; Pinned v1 workflows continue on v1.

Inspect generated HPAs:

```sh
kubectl --context kind-temporal-worker-hpa -n media-demo get hpa -o yaml
```

Each generated HPA has its own `scaleTargetRef` and Build ID selectors on both metrics. Those fields are empty or omitted in the source template because the Worker Controller supplies them.

## 4. Let work finish

The load Job stops submitting at its configured count. Wait for the `hpa-example-` workflows to complete in the Temporal UI. Activity utilization falls, and each HPA reduces its own version's replicas after the stabilization window. The controller handles drained-version cleanup according to the sunset delays.

Replica reduction is not a guarantee that every Activity gets to finish on its current pod. Show the Activity's heartbeats and retry policy in [main.go](sample-worker/main.go) if this comes up.

## Troubleshooting

| Symptom | Check |
| --- | --- |
| Worker image cannot be pulled | Load both tags into kind, or push to your cluster's registry |
| Worker cannot connect | Namespace API key, Cloud endpoint and namespace; inspect worker/controller logs |
| No HPA exists | WorkerResourceTemplate status and controller webhook/controller logs |
| HPA repeatedly resets replicas | Confirm WorkerDeployment omits both current and deprecated replicas fields |
| Slot metric is unknown | Worker SDK metrics endpoint, pod discovery, ActivityWorker/task_queue labels |
| Backlog metric is unknown | Metrics API key, Cloud scrape health, namespace and task queue filters; allow first Cloud emission and adapter discovery |
| Backlog has __unversioned__ label | Confirm worker versioning is enabled and the Cloud scrape requests version labels |
| Both versions scale as one | Check all three controller-injected identity selectors on both HPA metrics |
| Prometheus cannot start | Check temporal-cloud-metrics Secret exists in the Helm release namespace |
| External Metrics API is unavailable | Prometheus Adapter APIService and its TLS/configuration; check for an existing provider |
| Rollout gate fails | RolloutGate must be registered by the worker and able to execute its Activity |

The chart uses the new `WorkerDeployment`, `Connection` and `workerDeploymentRef` API names from the pinned controller release.

## Reset and remove

Wait for workflows to finish. You can repeat a finite batch; each batch uses distinct Workflow IDs. Return to v1 using a Helm upgrade with the v1 tag if you need to repeat the rollout.

To remove a local run:

```sh
make down
```

On an existing cluster, remove only the example release after its workflows finish:

```sh
helm uninstall media --kube-context your-context --namespace media-demo
```

The example has no recurring load: the CronJob remains suspended.
