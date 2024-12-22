.PHONY: all build test clean build deploy undeploy lint lint-yaml lint-all verify

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
	trap './scripts/delete-kind-cluster.sh' EXIT; \
	./scripts/create-kind-cluster.sh && \
	$(MAKE) build && \
	./scripts/kind-deploy.sh && \
	./scripts/integ-test.sh

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
