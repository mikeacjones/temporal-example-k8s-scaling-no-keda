#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p .run
helm lint --strict ./chart
helm template media ./chart --namespace media-demo -f values.example.yaml > .run/rendered.yaml
echo "Rendered chart: .run/rendered.yaml"
