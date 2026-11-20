#!/usr/bin/env python3
"""Builder utility: extract rendered config for promtool and review."""
from pathlib import Path
import yaml

root = Path(__file__).resolve().parents[1]
objects = list(yaml.safe_load_all((root / ".run/rendered.yaml").read_text()))
config = next(o for o in objects if o and o.get("kind") == "ConfigMap" and o["metadata"]["name"] == "media-worker-hpa-config")
for name, contents in config["data"].items():
    (root / ".run" / name).write_text(contents)
worker = next(o for o in objects if o and o.get("kind") == "WorkerDeployment")
wrt = next(o for o in objects if o and o.get("kind") == "WorkerResourceTemplate")
assert "replicas" not in worker["spec"]["deployment"], "HPA must own replicas"
assert "replicas" not in worker["spec"], "deprecated replicas must also be absent"
hpa = wrt["spec"]["template"]
assert hpa["spec"]["scaleTargetRef"] == {}, "controller must inject the target"
assert hpa["spec"]["metrics"][0]["external"]["target"]["type"] == "Value"
assert hpa["spec"]["metrics"][1]["external"]["target"]["type"] == "AverageValue"
for m in hpa["spec"]["metrics"]:
    labels = m["external"]["metric"]["selector"]["matchLabels"]
    assert not {"temporal_worker_deployment_name", "temporal_worker_build_id", "temporal_namespace"} & labels.keys()
assert not any(o and o.get("kind") == "ScaledObject" for o in objects)
cron = next(o for o in objects if o and o.get("kind") == "CronJob")
assert cron["spec"]["suspend"] is True
print("Extracted config; checked HPA ownership, version injection and finite load.")
