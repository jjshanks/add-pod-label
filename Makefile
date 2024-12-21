# Build variables
BINARY_NAME=pod-label-webhook
DOCKER_REPO=ghcr.io/jjshanks
IMAGE_NAME=$(DOCKER_REPO)/$(BINARY_NAME)
VERSION?=latest

# Kubernetes related variables
NAMESPACE=pod-label-system
KIND_CLUSTER_NAME=add-label-webhook

.PHONY: all build test clean docker-build docker-push dev-setup dev-cleanup deploy undeploy lint lint-yaml lint-all verify

# Default target
all: build

# Build using goreleaser
build:
	goreleaser build --snapshot --clean

# Run all tests
test:
	go test -v -race -cover ./...

# Run integration tests
test-integration:
	go test -v -tags=integration ./...

# Clean build artifacts
clean:
	rm -rf dist/
	go clean -testcache

# Build docker image using goreleaser
docker-build:
	goreleaser build --snapshot --clean

# Push docker image
docker-push: docker-build
	docker push $(IMAGE_NAME):$(VERSION)

# Development setup - creates kind cluster with cert-manager
dev-setup:
	kind create cluster --name $(KIND_CLUSTER_NAME) --config manifests/kind-config.yaml
	kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.14.4/cert-manager.yaml
	kubectl wait --for=condition=Ready --timeout=300s -n cert-manager deployment/cert-manager
	kubectl wait --for=condition=Ready --timeout=300s -n cert-manager deployment/cert-manager-cainjector
	kubectl wait --for=condition=Ready --timeout=300s -n cert-manager deployment/cert-manager-webhook

# Development cleanup
dev-cleanup:
	kind delete cluster --name $(KIND_CLUSTER_NAME)

# Deploy to local Kind cluster
deploy:
	kubectl create namespace $(NAMESPACE) --dry-run=client -o yaml | kubectl apply -f -
	kubectl apply -f manifests/webhook.yaml
	# Wait for cert-manager to generate the certificate
	kubectl wait --for=condition=Ready --timeout=60s -n $(NAMESPACE) certificate/pod-label-webhook-cert
	kubectl apply -f manifests/deployment.yaml

# Build and load image into Kind
dev-build: docker-build
	kind load docker-image $(IMAGE_NAME):$(VERSION) --name $(KIND_CLUSTER_NAME)

# Remove from Kubernetes
undeploy:
	kubectl delete -f manifests/deployment.yaml -f manifests/webhook.yaml --ignore-not-found
	kubectl delete namespace $(NAMESPACE) --ignore-not-found

# Run Go linting
lint:
	golangci-lint run ./...

# Run YAML linting
lint-yaml:
	yamllint .

# Run all linting
lint-all: lint lint-yaml

# Verify all checks pass (useful for pre-commit)
verify: lint-all test

# Help target
help:
	@echo "Available targets:"
	@echo "  build          - Build using goreleaser"
	@echo "  test           - Run unit tests"
	@echo "  test-integration - Run integration tests"
	@echo "  clean          - Clean build artifacts"
	@echo "  docker-build   - Build Docker image using goreleaser"
	@echo "  docker-push    - Push Docker image to registry"
	@echo "  dev-setup     - Create Kind cluster with cert-manager"
	@echo "  dev-cleanup   - Delete Kind cluster"
	@echo "  deploy        - Deploy webhook to cluster"
	@echo "  dev-build     - Build and load image into Kind"
	@echo "  undeploy      - Remove webhook from cluster"
	@echo "  lint          - Run Go linting"
	@echo "  lint-yaml     - Run YAML linting"
	@echo "  lint-all      - Run all linting checks"
	@echo "  verify        - Run all checks (linting and tests)"