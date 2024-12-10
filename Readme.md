# Pod Label Webhook

A Kubernetes admission controller that automatically adds the label `hello=world` to every pod created in non-system namespaces. Built with Go and uses cert-manager for TLS certificate management.

## Project Structure

```
pod-label-webhook/
├── .github/
│   ├── dependabot.yml       # Dependency update configuration
│   └── workflows/           # GitHub Actions workflows
│       ├── go.yml          # Main Go workflow
│       ├── quality.yml     # Code quality checks
│       ├── release.yml     # Release automation
│       └── security.yml    # Security scanning
├── .gitignore              # Git ignore file
├── .goreleaser.yml         # Release configuration
├── .yamllint.yml          # YAML linting configuration
├── Dockerfile             # Container image build instructions
├── Makefile              # Build and deployment automation
├── README.md             # This file
├── go.mod               # Go module definition
├── manifests/
│   ├── deployment.yaml    # Webhook deployment and service
│   ├── kind-config.yaml   # Kind cluster configuration
│   └── webhook.yaml       # Certificate and webhook configuration
└── pkg/
    └── webhook/
        ├── cmd/
        │   └── main.go    # Entry point
        └── webhook.go     # Main webhook logic
```

## Prerequisites

- Go 1.21+
- Docker
- Kind (Kubernetes in Docker)
- kubectl
- make

## Quick Start

1. Clone the repository:
```bash
git clone https://github.com/jjshanks/pod-label-webhook.git
cd pod-label-webhook
```

2. Create a test cluster and deploy the webhook:
```bash
make build           # Build the webhook image
make create-cluster  # Create Kind cluster and install cert-manager
make load           # Load the image into Kind
make deploy         # Deploy the webhook
```

3. Test the webhook:
```bash
make test           # Creates a test pod to verify labeling
```

## Development

### Available Make Commands

- `make help` - Show available commands
- `make create-cluster` - Creates a Kind cluster and installs cert-manager
- `make delete-cluster` - Deletes the Kind cluster
- `make build` - Builds the webhook Docker image
- `make load` - Loads the webhook image into Kind cluster
- `make deploy` - Deploys the webhook to the cluster
- `make test` - Creates a test pod to verify webhook
- `make debug` - Shows status of webhook deployment
- `make test-unit` - Runs unit tests
- `make test-integration` - Runs integration tests
- `make test-integration-debug` - Runs integration tests with debug output
- `make test-coverage` - Generates test coverage report
- `make test-all` - Runs all tests (unit, integration, coverage)
- `make clean-test` - Cleans up test artifacts

### GitHub Workflows

#### Main Go Workflow (`go.yml`)
- Runs on pull requests (excluding documentation changes)
- Formats Go code and organizes imports
- Runs Go vet checks
- Runs unit tests with race detection
- Generates coverage report
- Runs integration tests (on Dependabot PRs or PRs labeled with 'integration-test')
- Automatically commits code formatting changes
- Notifies on failures through PR comments

#### Quality Checks (`quality.yml`) 
- Runs on pull requests (excluding documentation changes)
- golangci-lint for Go code (only reports new issues)
- Strict YAML validation with yamllint
- Kubernetes manifest validation with kubeconform
- Enforces strict validation modes

#### Security Scanning (`security.yml`)
- Runs on:
  - Push to main branch
  - Pull requests
  - Weekly scheduled scans (Sundays)
- Multiple security scanners:
  - Gosec for Go security issues
  - Trivy for filesystem and container scanning
  - Anchore for container security analysis
- Results uploaded to GitHub Security tab
- Provides scan summaries in workflow logs

#### Release Automation (`release.yml`)
- Triggered by semantic version tags (v*.*.*)
- Creates GitHub releases with automated release notes
- Builds multi-arch container images (amd64, arm64)
- Publishes to GitHub Container Registry
- Automatically updates release documentation
- Uses GoReleaser for consistent releases

### Dependabot Configuration

Automatically updates:
- Go dependencies
- GitHub Actions
- Docker base images

Dependencies are checked weekly and updated via pull requests:
- Integration tests run automatically on Dependabot PRs
- Updates are grouped by ecosystem to reduce noise
- Security updates have higher priority

### Testing

The project includes several types of tests:

#### Unit Tests
```bash
make test-unit
```
Tests the webhook logic with race detection and generates coverage reports.

#### Integration Tests
```bash
make test-integration
```
Creates a test cluster and verifies the entire system works together.
Note: Runs automatically on:
- Dependabot pull requests
- Pull requests labeled with 'integration-test'

#### Coverage Report
```bash
make test-coverage
```
Generates a test coverage report in HTML format.
Coverage reports are automatically uploaded as artifacts in GitHub Actions.

### Common Tasks

#### Adding New Labels
Modify the `createPatch` function in `pkg/webhook/webhook.go`:
```go
func createPatch(pod *corev1.Pod) ([]byte, error) {
    var patch []patchOperation
    labels := map[string]string{
        "hello": "world",
        // Add more labels here
        "new-label": "value",
    }
    // ... rest of the function
}
```

#### Modifying Namespace Exclusions
Update the `namespaceSelector` in `manifests/webhook.yaml`:
```yaml
namespaceSelector:
  matchExpressions:
  - key: kubernetes.io/metadata.name
    operator: NotIn
    values: 
    - kube-system
    - cert-manager
    - pod-label-system
    # Add more namespaces here
```

## Configuration

### Webhook Configuration
- Port: 8443
- Certificate Path: `/etc/webhook/certs`
- Excluded Namespaces: kube-system, cert-manager, pod-label-system

### Certificates
- Managed by cert-manager
- Auto-renewed
- Stored in Kubernetes secrets

## Releases

To create a new release:
1. Create and push a tag following semantic versioning:
```bash
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

2. The release workflow will automatically:
- Create a GitHub release
- Build and push multi-arch container images
- Generate release notes
- Create binary artifacts
- Update documentation

## Contributing

1. Fork the repository
2. Create a feature branch
3. Add tests for new functionality
4. Ensure all tests pass: `make test-all`
5. Ensure code quality checks pass
6. Ensure security scans pass
7. Submit a pull request

Note: Integration tests will run automatically if you add the 'integration-test' label to your PR.

## License

MIT

## AI Assistance

If you want to work on this project with an AI assistant, you can use the following prompt:

```
I'm working on a Kubernetes admission controller project that adds the label "hello=world" to every pod. The project structure is:

/pod-label-webhook
  /pkg
    /webhook
      /cmd
        main.go         # Entry point
      webhook.go        # Main webhook logic
  /manifests
    deployment.yaml    # Webhook deployment and service
    webhook.yaml       # Certificate and webhook configuration
    kind-config.yaml   # Kind cluster configuration
  Dockerfile          # Multi-stage build
  Makefile           # Build automation
  go.mod             # Go module file

The project uses:
- Go 1.21+ for implementation
- cert-manager for TLS certificate management
- kind for local testing
- Make for build automation
- Docker for containerization

The webhook:
- Adds hello=world label to all pods
- Uses cert-manager for TLS
- Excludes system namespaces (kube-system, cert-manager, pod-label-system)
- Implements MutatingWebhookConfiguration

I need help with [specific task/issue]. Can you assist with [specific question]?
```

## Troubleshooting

### Common Issues

1. `make deploy` fails:
   - Ensure cert-manager is running: `kubectl get pods -n cert-manager`
   - Check webhook logs: `kubectl logs -n pod-label-system deployment/pod-label-webhook`
   - Verify certificates: `kubectl get certificate -n pod-label-system`

2. Test pod not getting labeled:
   - Check webhook is running: `make debug`
   - Verify namespace is not excluded
   - Check webhook logs for errors

3. Integration tests fail:
   - Ensure no existing test clusters: `make clean-test`
   - Try debug mode: `make test-integration-debug`
   - Check system resources

4. GitHub Actions failing:
   - Check golangci-lint output for new issues
   - Verify YAML files pass strict validation
   - Ensure Kubernetes manifests are valid
   - Review security scan results in GitHub Security tab
   - Check workflow logs for specific errors
   - For dependabot PRs, verify integration tests are passing
   - Ensure all quality checks pass before merging
   - Address high severity security findings
   - Verify tag format for releases (v*.*.*)