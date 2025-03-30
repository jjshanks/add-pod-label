# Pod Label Webhook Development Guide

## Build & Test Commands
- Build: `make build`
- Test all: `make test` 
- Run single test: `go test -v ./internal/webhook -run TestCreatePatch`
- Integration tests: `make test-integration`
- Fuzz tests: `make fuzz` or `go test -fuzz=FuzzCreatePatch -fuzztime=1m ./internal/webhook/`
- Lint: `make lint`
- Format code: `make fmt`
- Pre-commit check: `make verify`

## Code Style Guidelines
- Use structured error types with appropriate context (see error.go)
- Context-aware logging with zerolog (`.With()` and `.Logger()` pattern)
- Comprehensive documentation with package/function comments
- Import order: standard lib → third-party → internal packages
- Table-driven tests with testify assertions
- Use clock interface for time-based tests
- Structured metrics recording for operations
- Constants defined at package level with descriptive comments
- Verbose debug logging with appropriate context fields