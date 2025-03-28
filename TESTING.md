# Testing

## Unit Tests

The project uses Go's testing framework along with testify for assertions. Some key features of the testing approach:

### Clock Interface

For time-dependent tests (like health checks), the project uses a clock interface to make testing deterministic:

```go
type Clock interface {
    Now() time.Time
}
```

This enables:

- Deterministic testing of time-based behavior
- No reliance on actual time passing during tests
- Easy simulation of various timing scenarios
- Fast and reliable test execution

## Integration Tests

Integration tests verify the webhook's behavior in a real Kubernetes environment using Kind. The tests:

1. Create a Kind cluster with required components
2. Deploy the webhook with proper certificates
3. Verify health endpoints using port forwarding (port 18443)
4. Test pod label modification behavior
5. Clean up all resources after completion

To run tests:

```bash
# Run unit tests
make test

# Run integration tests (requires Kind)
make test-integration
```

## Health Check Testing

The health endpoints can be tested manually using curl:

```bash
# Test liveness probe
curl -sk https://localhost:18443/healthz

# Test readiness probe
curl -sk https://localhost:18443/readyz
```

Health probes are configured in the deployment manifest and use HTTPS. The probes verify:

- Liveness: Server is responsive and hasn't deadlocked
- Readiness: Server is initialized and ready to handle requests

## Fuzz Testing

The project includes fuzz testing to identify edge cases and potential vulnerabilities:

### Available Fuzz Tests
- `FuzzCreatePatch`: Tests pod label mutation with fuzzed inputs
- `FuzzHandleMutate`: Tests webhook request handling with fuzzed admission reviews

Fuzz tests can be run using:
```bash
# Run all fuzz tests for 1 minute
make fuzz

# Run extended fuzz tests for 5 minutes
make fuzz-long

# Run specific fuzz test with custom duration
go test -fuzz=FuzzCreatePatch -fuzztime=10m ./internal/webhook/
```

Fuzz testing helps identify:
- Input validation issues
- Encoding/parsing bugs
- Edge cases in label handling
- Memory safety issues
- Security vulnerabilities

Failed fuzz test inputs are saved to `testdata/fuzz/` and can be replayed:
```bash
go test -run=FuzzCreatePatch/SEED_HERE
```

## Adding New Tests

When adding new features:

1. Add unit tests using the provided test utilities and clock interface when dealing with time
2. Update integration tests if the feature affects pod mutation behavior
3. Document test cases in comments
4. Run both unit and integration tests before submitting changes
