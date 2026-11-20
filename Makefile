.PHONY: deps render image test cluster bootstrap install load watch down
KUBE_CONTEXT ?= kind-temporal-worker-hpa
NAMESPACE ?= media-demo
RELEASE ?= media
TAG ?= v1
VALUES ?= values.local.yaml

deps:
	helm dependency build ./chart

render:
	bash scripts/render.sh

image:
	docker build -t temporal-hpa-worker:$(TAG) ./sample-worker

test: render
	docker build -f tests/Dockerfile.validation -t temporal-hpa-validator .
	docker run --rm -v "$(CURDIR):/work" temporal-hpa-validator scripts/extract-config.py
	docker run --rm -v "$(CURDIR):/work:ro" temporal-hpa-validator scripts/validate-schemas.py
	docker build --target test -t temporal-hpa-worker:test ./sample-worker
	docker run --rm --entrypoint promtool -v "$(CURDIR):/work:ro" -w /work prom/prometheus:v3.16.0 test rules tests/slots.test.yml

cluster:
	mkdir -p .run
	kind create cluster --config cluster/kind.yaml --kubeconfig .run/kubeconfig

bootstrap:
	bash scripts/bootstrap.sh $(KUBE_CONTEXT)

install:
	helm upgrade --install $(RELEASE) ./chart --kube-context $(KUBE_CONTEXT) --namespace $(NAMESPACE) --create-namespace -f $(VALUES) --wait --timeout 5m

load:
	kubectl --context $(KUBE_CONTEXT) -n $(NAMESPACE) create job --from=cronjob/$(RELEASE)-load $(RELEASE)-load-$(shell date +%s)

watch:
	kubectl --context $(KUBE_CONTEXT) -n $(NAMESPACE) get deployments,hpa -w

down:
	kind delete cluster --name temporal-worker-hpa
