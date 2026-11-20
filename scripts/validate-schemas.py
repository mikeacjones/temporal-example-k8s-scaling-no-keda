#!/usr/bin/env python3
"""Validate controller resources against the pinned CRDs and the generated HPA."""
import json
from pathlib import Path
import urllib.request
import yaml
from jsonschema import Draft7Validator

root = Path(__file__).resolve().parents[1]
objects = [o for o in yaml.safe_load_all((root / ".run/rendered.yaml").read_text()) if o]
base = "https://raw.githubusercontent.com/temporalio/temporal-worker-controller/56318aa53962a8f7127bf07e9e3d31c855cb593b/helm/temporal-worker-controller-crds/templates/"
files = {"Connection": "connections", "WorkerDeployment": "workerdeployments", "WorkerResourceTemplate": "workerresourcetemplates"}
for kind, plural in files.items():
    url = base + "temporal.io_" + plural + ".yaml"
    with urllib.request.urlopen(url, timeout=30) as response:
        crd = yaml.safe_load(response.read())
    version = next(v for v in crd["spec"]["versions"] if v["name"] == "v1alpha1")
    schema = version["schema"]["openAPIV3Schema"]
    obj = next(o for o in objects if o["kind"] == kind)
    Draft7Validator(schema).validate(obj)
    print(kind + ": pinned CRD structural schema valid")

wrt = next(o for o in objects if o["kind"] == "WorkerResourceTemplate")
hpa = wrt["spec"]["template"]
hpa["metadata"] = {"name": "media-v1-hpa", "namespace": "media-demo"}
hpa["spec"]["scaleTargetRef"] = {"apiVersion": "apps/v1", "kind": "Deployment", "name": "media-v1"}
for m in hpa["spec"]["metrics"]:
    m["external"]["metric"]["selector"]["matchLabels"].update({
        "temporal_worker_deployment_name": "media-demo_media",
        "temporal_worker_build_id": "v1", "temporal_namespace": "example.a1b2c",
    })
url = "https://raw.githubusercontent.com/yannh/kubernetes-json-schema/master/v1.35.0-standalone-strict/horizontalpodautoscaler-autoscaling-v2.json"
with urllib.request.urlopen(url, timeout=30) as response:
    schema = json.load(response)
Draft7Validator(schema).validate(hpa)
print("Generated per-version HPA: Kubernetes v1.35 structural schema valid")
print("Structural validation does not execute Kubernetes CEL rules or admission webhooks.")
