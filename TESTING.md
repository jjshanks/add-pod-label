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

# Run integration tests with tracing enabled (requires Kind)
make test-integration-trace
```

### Tracing Tests

The tracing integration tests verify OpenTelemetry tracing functionality:

1. Deploy an OpenTelemetry collector alongside the webhook
2. Configure the webhook to send traces to the collector
3. Generate webhook traffic by creating test pods
4. Verify traces are collected by the OpenTelemetry collector
5. Validate span attributes and parent-child relationships

**Note:** When using tracing in your own environment, deploy an OpenTelemetry collector alongside the webhook. For detailed setup instructions, refer to the OpenTelemetry Collector documentation at https://opentelemetry.io/docs/collector/.

#### Tracing Flow Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                       Kubernetes API Server                          │
└───────────────────────────────────┬─────────────────────────────────┘
                                    │ AdmissionReview
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│                             Webhook Server                           │
│                                                                      │
│  ┌─────────────┐   ┌──────────────┐   ┌───────────────┐             │
│  │    Label    │   │   Tracing    │   │    Metrics    │    HTTP     │
│  │ Middleware  │──▶│  Middleware  │──▶│  Middleware   │──▶ Handler  │
│  └─────────────┘   └──────────────┘   └───────────────┘             │
│         │                 │                                          │
│         │                 │ Creates spans                            │
│         │ Adds context    │                                          │
│         │                 ▼                                          │
│         │        ┌──────────────────────┐                            │
│         └───────▶│  OpenTelemetry Span  │                            │
│                  │  - HTTP attributes   │                            │
│                  │  - Pod attributes    │                            │
│                  │  - Status codes      │                            │
│                  └──────────┬───────────┘                            │
│                             │                                        │
└─────────────────────────────┼────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      OpenTelemetry Collector                         │
│                                                                      │
│  ┌─────────────┐    ┌─────────────┐    ┌─────────────────────────┐  │
│  │   Receiver  │───▶│  Processor  │───▶│ Logging/Debug Exporter  │  │
│  └─────────────┘    └─────────────┘    └─────────────────────────┘  │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

#### Tracing Propagation and Context Flow

1. **Request Arrival**:
   - Kubernetes API server sends admission review to webhook
   - HTTP request is received by middleware chain

2. **Label Middleware** (First in chain):
   - Extracts pod information from URL parameters:
     - `pod`: The pod name
     - `namespace`: The pod's namespace
     - `prefix`: Custom label prefix to use
   - Stores information in request context
   - Passes enriched context to next middleware

3. **Tracing Middleware** (Second in chain):
   - Extracts trace context from request headers
   - Creates a new span for the HTTP request
   - Reads pod information from context
   - Adds HTTP and pod attributes to span
   - Propagates context with active span to next middleware

4. **Handler Processing**:
   - Creates child spans for key operations
   - Adds operation-specific attributes
   - Records timing for critical sections
   - Properly handles span errors and status

5. **Span Export**:
   - Completed spans sent to OpenTelemetry collector
   - Collector processes and exports traces
   - Trace data available for analysis

#### Span Hierarchy

The following spans are verified in the trace tests:

- **HTTP Request Span** (parent):
  - Created by tracing middleware
  - Contains request method, path, status code
  - Includes pod name and namespace from context
  - Duration covers full request lifecycle

- **Handler Spans** (children):
  - **Admission Review**: Processing the admission request
    - **Decoding/Unmarshalling**: Parsing admission review JSON
    - **Authorization**: Checking permissions (if enabled)
    - **Patch Creation**: Generating the JSON patch
      - **Label Operations**: Individual label modifications
    - **Response Preparation**: Creating admission response

#### Testing Span Attributes

During testing, verify that spans contain the following attributes:

- **HTTP Request Span**:
  - `http.method`: The HTTP method (POST)
  - `http.path`: Request path (/mutate)
  - `http.status_code`: Response status (200)
  - `pod.name`: Name of pod being processed (if available)
  - `pod.namespace`: Namespace of pod (if available)
  - `label.prefix`: Prefix used for labels (if specified)
  - `request.id`: Request ID from X-Request-ID header (if present)

- **Operation Spans**:
  - `operation`: Type of operation being performed
  - `pod.name`: Name of pod being processed
  - `pod.namespace`: Namespace of pod
  - `pod.uid`: UID of pod from admission review

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

### Testing Middleware Components

When adding or modifying middleware components:

1. **Isolated Tests**: Create dedicated tests for the middleware in isolation
   ```go
   func TestMyMiddleware(t *testing.T) {
       // Create minimal server
       server := &Server{logger: zerolog.Nop()}
       
       // Test handler to verify behavior
       testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
           // Verify middleware effects
           w.WriteHeader(http.StatusOK)
       })
       
       // Wrap with middleware
       handler := server.myMiddleware(testHandler)
       
       // Test with different scenarios
       req := httptest.NewRequest("GET", "/test", nil)
       rec := httptest.NewRecorder()
       handler.ServeHTTP(rec, req)
       
       // Assertions...
   }
   ```

2. **Middleware Chain Tests**: Verify integration with the middleware chain
   ```go
   func TestMiddlewareChaining(t *testing.T) {
       // Create middleware chain in correct order
       handler := server.firstMiddleware(
           server.secondMiddleware(
               server.thirdMiddleware(testHandler)))
       
       // Test request flow through entire chain
       // ...
   }
   ```

3. **Context Propagation Tests**: Ensure context values flow through the chain
   - Test that context values set by one middleware are accessible by subsequent middleware
   - Verify that handlers can access all context values

4. **Edge Cases**:
   - Test with empty/missing parameters
   - Test with malformed inputs
   - Test with unusual values (very long strings, special characters)
   - Test with concurrent requests

5. **Performance Impact**:
   - Consider benchmark tests for middleware that might impact performance
   - Measure overhead of middleware processing
