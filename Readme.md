# Pod Label Webhook

A Kubernetes admission webhook that automatically adds labels to pods during creation.

## Overview

This webhook intercepts pod creation requests in Kubernetes and adds a "hello: world" label to all pods. It's built using the Kubernetes admission webhook framework and can be deployed as a standalone service in your cluster.

## Features

- Automatically labels pods during creation
- Secure TLS communication using cert-manager
- Configurable logging levels and formats
- Kubernetes native deployment
- Multi-architecture support (amd64, arm64)

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

2. Set up a local Kind cluster with cert-manager:
```bash
make dev-setup
```

3. Build and load the webhook image:
```bash
make dev-build
```

4. Deploy the webhook:
```bash
make deploy
```

### Production Deployment

The webhook can be installed using the provided Kubernetes manifests:

```bash
kubectl create namespace pod-label-system
kubectl apply -f manifests/
```

Pre-built images are available from GitHub Container Registry:
```bash
ghcr.io/jjshanks/pod-label-webhook:latest
```

## Configuration

The webhook supports the following configuration options:

| Flag | Environment Variable | Default | Description |
|------|---------------------|---------|-------------|
| --address | WEBHOOK_ADDRESS | 0.0.0.0:8443 | The address to listen on |
| --log-level | WEBHOOK_LOG_LEVEL | info | Log level (trace, debug, info, warn, error, fatal, panic) |
| --console | WEBHOOK_CONSOLE | false | Use console log format instead of JSON |
| --config | - | $HOME/.webhook.yaml | Path to config file |

## Development

### Project Structure
```
├── pkg/webhook/      # Core webhook implementation
│   ├── cmd/         # Command line interface
│   ├── webhook.go   # Main webhook logic
│   └── *_test.go    # Tests
├── manifests/       # Kubernetes deployment manifests
└── Dockerfile       # Container build definition
```

### Available Make Targets

- `make build` - Build using goreleaser
- `make test` - Run unit tests
- `make test-integration` - Run integration tests
- `make clean` - Clean build artifacts
- `make docker-build` - Build Docker image using goreleaser
- `make docker-push` - Push Docker image to registry
- `make dev-setup` - Create Kind cluster with cert-manager
- `make dev-cleanup` - Delete Kind cluster
- `make deploy` - Deploy webhook to cluster
- `make dev-build` - Build and load image into Kind
- `make undeploy` - Remove webhook from cluster
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

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the terms in the LICENSE file.