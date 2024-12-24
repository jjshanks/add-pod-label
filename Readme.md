# Pod Label Webhook

A Kubernetes admission webhook that automatically adds labels to pods during creation, with configurable behavior via annotations.

## Overview

This webhook intercepts pod creation requests in Kubernetes and adds a "hello: world" label to pods unless explicitly disabled via annotation. It's built using the Kubernetes admission webhook framework and can be deployed as a standalone service in your cluster.

## Features

- Automatically labels pods during creation
- Configurable behavior using annotations
- Secure TLS communication using cert-manager
- Configurable logging levels and formats
- Kubernetes native deployment
- Multi-architecture support (amd64, arm64)

## Usage

By default, the webhook adds a "hello: world" label to all pods. This behavior can be controlled using the following annotation:

```yaml
pod-label-webhook.jjshanks.github.com/add-hello-world: "false"
```

Example deployment with labeling disabled:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: example
spec:
  template:
    metadata:
      annotations:
        pod-label-webhook.jjshanks.github.com/add-hello-world: "false"
    spec:
      containers:
        - name: nginx
          image: nginx
```

## Prerequisites

- Go 1.23+
- Docker
- Kind (for local development)
- kubectl
- goreleaser
- cert-manager

## Installation

### Local Development

1. Clone the repository:

```bash
git clone https://github.com/jjshanks/pod-label-webhook.git
cd pod-label-webhook
```

2. Build and run tests:

```bash
make build
make test
```

3. Run integration tests:

```bash
make test-integration
```

### Production Deployment

The webhook can be installed using the provided Kubernetes manifests:

```bash
kubectl create namespace webhook-test
kubectl apply -f manifests/
```

Pre-built images are available from GitHub Container Registry:

```bash
ghcr.io/jjshanks/pod-label-webhook:latest
```

## Configuration

The webhook supports the following configuration options:

| Flag        | Environment Variable | Default             | Description                                               |
| ----------- | -------------------- | ------------------- | --------------------------------------------------------- |
| --address   | WEBHOOK_ADDRESS      | 0.0.0.0:8443        | The address to listen on                                  |
| --log-level | WEBHOOK_LOG_LEVEL    | info                | Log level (trace, debug, info, warn, error, fatal, panic) |
| --console   | WEBHOOK_CONSOLE      | false               | Use console log format instead of JSON                    |
| --config    | -                    | $HOME/.webhook.yaml | Path to config file                                       |

## Development

### Project Structure

```
├── pkg/webhook/      # Core webhook implementation
│   ├── cmd/         # Command Line Interface
│   ├── webhook.go   # Main webhook logic
│   └── *_test.go    # Tests
├── tests/           # Test resources
│   ├── manifests/   # Test deployment manifests
│   └── scripts/     # Testing scripts
└── Dockerfile       # Container build definition
```

### Available Make Targets

- `make build` - Build using goreleaser
- `make test` - Run unit tests
- `make test-integration` - Run integration tests
- `make clean` - Clean build artifacts
- `make lint` - Run Go linting
- `make lint-yaml` - Run YAML linting
- `make verify` - Run all checks (linting and tests)

### Integration Tests

Integration tests use shell scripts to create a Kind cluster, deploy the webhook, and verify its functionality. The tests:

1. Create a Kind cluster with cert-manager
2. Build and deploy the webhook
3. Create test deployments with and without the annotation
4. Verify the webhook correctly handles both cases:
   - Adds label when annotation is absent or true
   - Skips label when annotation is false

Integration tests can be triggered in pull requests by:

1. Adding the 'integration-test' label to the PR
2. Including '#integ-test' in any commit message
3. Including '#integ-test' in the PR title
4. Automatically for Dependabot PRs

### Release Process

Releases are automated using GitHub Actions. To create a new release:

1. Create and push a new tag following semantic versioning:

   ```bash
   git tag v1.0.0
   git push origin v1.0.0
   ```

2. The release workflow will automatically:
   - Build multi-architecture binaries
   - Create Docker images
   - Publish the release on GitHub
   - Push images to GitHub Container Registry

Alternatively, you can trigger a manual release through the GitHub Actions UI.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the terms in the LICENSE file.
