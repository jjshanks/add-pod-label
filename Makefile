.PHONY: all build test clean build deploy undeploy lint lint-yaml lint-all verify fuzz fmt test-integration-trace

# Default target
all: build

# Build using goreleaser
build:
	goreleaser release --snapshot --clean

# Run all tests
test:
	go test -v -race -cover ./...

# Run integration tests
test-integration:
	trap './test/integration/delete-kind-cluster.sh' EXIT; \
	./test/integration/create-kind-cluster.sh && \
	$(MAKE) build && \
	./test/integration/kind-deploy.sh && \
	./test/integration/integ-test.sh

# Run integration tests with tracing enabled
test-integration-trace:
	trap './test/integration/delete-kind-cluster.sh' EXIT; \
	./test/integration/create-kind-cluster.sh && \
	$(MAKE) build && \
	./test/integration/kind-deploy-trace.sh && \
	./test/integration/integ-test-trace.sh

# Run fuzz tests (default 1m duration)
fuzz:
	go test -fuzz=FuzzCreatePatch -fuzztime=1m ./internal/webhook/
	go test -fuzz=FuzzHandleMutate -fuzztime=1m ./internal/webhook/

# Run fuzz tests for a longer duration (5m)
fuzz-long:
	go test -fuzz=FuzzCreatePatch -fuzztime=5m ./internal/webhook/
	go test -fuzz=FuzzHandleMutate -fuzztime=5m ./internal/webhook/

# Clean build artifacts
clean:
	rm -rf dist/
	go clean -testcache

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

# Format Go code using goimports
fmt:
	goimports -local github.com/jjshanks/pod-label-webhook -w .