# Variables
CLUSTER_NAME := webhook-test
IMAGE_NAME := pod-label-webhook
IMAGE_TAG := latest
KIND_CONFIG := manifests/kind-config.yaml

# Default target
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  create-cluster  - Creates a new kind cluster and installs cert-manager"
	@echo "  delete-cluster  - Deletes the kind cluster"
	@echo "  build          - Builds the webhook Docker image"
	@echo "  load           - Loads the webhook image into kind cluster"
	@echo "  deploy         - Deploys the webhook to the cluster"
	@echo "  test           - Creates a test pod to verify webhook"
	@echo "  debug          - Shows status of webhook deployment"
	@echo "  all            - Builds, creates cluster, loads image, and deploys"

.PHONY: create-cluster
create-cluster:
	@echo "Creating kind cluster..."
	kind create cluster --name $(CLUSTER_NAME) --config $(KIND_CONFIG)
	@echo "Installing cert-manager..."
	kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.3/cert-manager.yaml
	@echo "Waiting for cert-manager namespace to be ready..."
	sleep 15
	@echo "Waiting for cert-manager pods..."
	kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=cert-manager -n cert-manager --timeout=90s
	kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=webhook -n cert-manager --timeout=90s
	kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=cainjector -n cert-manager --timeout=90s
	@echo "Cert-manager installation complete"

.PHONY: delete-cluster
delete-cluster:
	@echo "Deleting kind cluster..."
	kind delete cluster --name $(CLUSTER_NAME)

.PHONY: build
build:
	@echo "Building webhook image..."
	docker build -t $(IMAGE_NAME):$(IMAGE_TAG) .

.PHONY: load
load: build
	@echo "Loading image into kind cluster..."
	kind load docker-image $(IMAGE_NAME):$(IMAGE_TAG) --name $(CLUSTER_NAME)

.PHONY: deploy
deploy:
	@echo "Deploying webhook..."
	kubectl apply -f manifests/webhook.yaml
	@echo "Waiting for webhook namespace to be created..."
	sleep 5
	@echo "Deploying webhook deployment and service..."
	kubectl apply -f manifests/deployment.yaml
	@echo "Waiting for webhook pod to be ready..."
	kubectl wait --for=condition=ready pod -l app=pod-label-webhook -n pod-label-system --timeout=90s
	@echo "Waiting for webhook to be fully operational..."
	sleep 10
	@echo "Deployment complete"

.PHONY: test
test:
	@echo "Creating test pod..."
	kubectl delete pod nginx --ignore-not-found
	sleep 2
	kubectl run nginx --image=nginx
	@echo "Waiting for pod to be ready..."
	kubectl wait --for=condition=ready pod/nginx --timeout=30s
	@echo "Checking pod labels..."
	kubectl get pod nginx --show-labels

.PHONY: debug
debug:
	@echo "Checking pod status..."
	kubectl get pods -n pod-label-system
	@echo "\nChecking deployment status..."
	kubectl describe deployment pod-label-webhook -n pod-label-system
	@echo "\nChecking events..."
	kubectl get events -n pod-label-system --sort-by='.lastTimestamp'
	@echo "\nChecking webhook logs..."
	kubectl logs -n pod-label-system deployment/pod-label-webhook

.PHONY: all
all: build create-cluster load deploy
	@echo "Waiting for system to stabilize..."
	sleep 10
	@echo "Setup complete. You can now run 'make test' to verify the webhook."

.PHONY: redeploy
redeploy: build load
	kubectl delete -f manifests/deployment.yaml || true
	kubectl delete -f manifests/webhook.yaml || true
	sleep 5
	make deploy

# Test targets
.PHONY: test-unit
test-unit:
	@echo "Running unit tests..."
	go test ./pkg/webhook -v -count=1 -short

.PHONY: test-integration-debug
test-integration-debug:
	@echo "Running integration tests with debug output..."
	go test ./pkg/webhook -v -count=1 -tags=integration -run TestWebhookIntegration -timeout 10m

.PHONY: test-integration
test-integration:
	@echo "Running integration tests..."
	-go test ./pkg/webhook -v -count=1 -tags=integration -run TestWebhookIntegration
	@echo "Cleaning up..."
	$(MAKE) clean-test

.PHONY: test-coverage
test-coverage:
	@echo "Running tests with coverage..."
	go test ./pkg/webhook -v -count=1 -short -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated in coverage.html"

.PHONY: test-all
test-all: clean-test test-unit test-coverage test-integration
	@echo "All tests completed"

.PHONY: clean-test
clean-test:
	@echo "Cleaning up test artifacts..."
	-kind delete cluster --name webhook-test 2>/dev/null || true
	-rm -f coverage.out coverage.html
	@echo "Test cleanup completed"