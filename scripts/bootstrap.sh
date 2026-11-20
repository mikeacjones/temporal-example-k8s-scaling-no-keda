#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
context=${1:?Usage: bash scripts/bootstrap.sh KUBERNETES_CONTEXT}
helm_args=(--kube-context "$context")
kubectl_args=(--context "$context")

# Both adapters register the same external.metrics.k8s.io APIService.
if kubectl "${kubectl_args[@]}" get apiservice v1beta1.external.metrics.k8s.io >/dev/null 2>&1; then
  echo "An External Metrics API provider already exists. Reuse it; do not install a second adapter."
  echo "See README.md: Use an existing monitoring stack."
  exit 1
fi

helm upgrade --install cert-manager oci://quay.io/jetstack/charts/cert-manager \
  --version v1.21.3 -n cert-manager --create-namespace \
  --set crds.enabled=true --wait --timeout 5m "${helm_args[@]}"
helm upgrade --install temporal-worker-controller-crds \
  oci://docker.io/temporalio/temporal-worker-controller-crds \
  --version 0.32.0 -n temporal-system --create-namespace "${helm_args[@]}"
kubectl "${kubectl_args[@]}" wait --for=condition=Established --timeout=60s \
  crd/connections.temporal.io crd/workerdeployments.temporal.io crd/workerresourcetemplates.temporal.io
helm upgrade --install temporal-worker-controller \
  oci://docker.io/temporalio/temporal-worker-controller \
  --version 0.32.0 -n temporal-system \
  -f cluster/controller-values.yaml --wait --timeout 5m "${helm_args[@]}"
kubectl "${kubectl_args[@]}" -n temporal-system wait --for=condition=Ready \
  certificates --all --timeout=120s
echo "Controller installed. Create the credentials Secrets, then install ./chart."
