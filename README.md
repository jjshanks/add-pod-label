# Add Pod Label

[![Go Report Card](https://goreportcard.com/badge/github.com/jjshanks/add-pod-label)](https://goreportcard.com/report/github.com/jjshanks/add-pod-label)
[![Go](https://github.com/jjshanks/add-pod-label/workflows/Go/badge.svg)](https://github.com/jjshanks/add-pod-label/actions?query=workflow%3AGo)
[![Quality](https://github.com/jjshanks/add-pod-label/workflows/Quality/badge.svg)](https://github.com/jjshanks/add-pod-label/actions?query=workflow%3AQuality)
[![Security](https://github.com/jjshanks/add-pod-label/workflows/Security/badge.svg)](https://github.com/jjshanks/add-pod-label/actions?query=workflow%3ASecurity)
[![Release](https://github.com/jjshanks/add-pod-label/workflows/Release/badge.svg)](https://github.com/jjshanks/add-pod-label/actions?query=workflow%3ARelease)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

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
add-pod-label.jjshanks.github.com/add-hello-world: "false"
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
        add-pod-label.jjshanks.github.com/add-hello-world: "false"
    spec:
      containers:
        - name: nginx
          image: nginx
```

## Prerequisites

- Go 1.24+
- Docker
- Kind (for local development)
- kubectl
- goreleaser
- cert-manager

## Installation

### Local Development

1. Clone the repository:

```bash
git clone https://github.com/jjshanks/add-pod-label.git
cd add-pod-label
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
ghcr.io/jjshanks/add-pod-label:latest
```

## Configuration

The webhook supports the following configuration options:

| Flag        | Environment Variable | Default             | Description                                               |
| ----------- | -------------------- | ------------------- | --------------------------------------------------------- |
| --address   | WEBHOOK_ADDRESS      | 0.0.0.0:8443        | The address to listen on                                  |
| --log-level | WEBHOOK_LOG_LEVEL    | info                | Log level (trace, debug, info, warn, error, fatal, panic) |
| --console   | WEBHOOK_CONSOLE      | false               | Use console log format instead of JSON                    |
| --config    | -                    | $HOME/.webhook.yaml | Path to config file                                       |

## Health Monitoring

The webhook implements health checks via HTTP endpoints:

- **Liveness Probe** (`/healthz`): Verifies the server is responsive

  - Returns 200 OK if the server is alive
  - Returns 503 if no successful health check in the last 60 seconds
  - Used by Kubernetes to determine if the pod should be restarted

- **Readiness Probe** (`/readyz`): Verifies the server is ready to handle requests
  - Returns 200 OK if the server is ready to handle requests
  - Returns 503 if the server is starting up or not ready
  - Used by Kubernetes to determine if traffic should be sent to the pod

The probes are configured with the following default settings:

```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 8443
    scheme: HTTPS
  initialDelaySeconds: 5
  periodSeconds: 10
  timeoutSeconds: 5
  failureThreshold: 3

readinessProbe:
  httpGet:
    path: /readyz
    port: 8443
    scheme: HTTPS
  initialDelaySeconds: 5
  periodSeconds: 10
  timeoutSeconds: 5
  failureThreshold: 3
```

You can customize these settings by modifying the deployment manifest.

## Development

### Project Structure

```
├── cmd/             # Command line interface
│   └── webhook/     # Main webhook command
│       └── main.go  # Entry point
├── internal/        # Private implementation code
│   ├── config/      # Configuration handling
│   └── webhook/     # Core webhook implementation
│       ├── webhook.go   # Main webhook logic
│       ├── server.go    # Server implementation
│       ├── metrics.go   # Metrics collection
│       ├── health.go    # Health checking
│       ├── error.go     # Error types
│       ├── clock.go     # Time utilities
│       └── *_test.go    # Tests
├── pkg/             # Public API packages
│   └── k8s/         # Kubernetes utilities
├── test/            # Test resources
│   ├── e2e/         # End-to-end tests
│   │   └── manifests/  # Test deployment manifests
│   └── integration/ # Integration test scripts
├── dashboards/      # Grafana dashboards
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

### Tests

Please see [TESTING.md](TESTING.md)

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the terms in the LICENSE file.
