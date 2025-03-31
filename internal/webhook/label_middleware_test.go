package webhook

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestLabelMiddleware(t *testing.T) {
	tests := []struct {
		name               string
		url                string
		expectedPodName    string
		expectedNamespace  string
		expectedLabelPrefix string
	}{
		{
			name:               "with all parameters",
			url:                "/mutate?pod=test-pod&namespace=test-ns&prefix=app.kubernetes.io/",
			expectedPodName:    "test-pod",
			expectedNamespace:  "test-ns",
			expectedLabelPrefix: "app.kubernetes.io/",
		},
		{
			name:               "with pod and namespace only",
			url:                "/mutate?pod=test-pod&namespace=test-ns",
			expectedPodName:    "test-pod",
			expectedNamespace:  "test-ns",
			expectedLabelPrefix: "",
		},
		{
			name:               "with pod only",
			url:                "/mutate?pod=test-pod",
			expectedPodName:    "test-pod",
			expectedNamespace:  "",
			expectedLabelPrefix: "",
		},
		{
			name:               "with namespace only",
			url:                "/mutate?namespace=test-ns",
			expectedPodName:    "",
			expectedNamespace:  "test-ns",
			expectedLabelPrefix: "",
		},
		{
			name:               "with no parameters",
			url:                "/mutate",
			expectedPodName:    "",
			expectedNamespace:  "",
			expectedLabelPrefix: "",
		},
		{
			name:               "with empty parameters",
			url:                "/mutate?pod=&namespace=&prefix=",
			expectedPodName:    "",
			expectedNamespace:  "",
			expectedLabelPrefix: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a server with minimal configuration
			server := &Server{
				logger: zerolog.Nop(),
			}

			// Create a test handler that extracts and validates values from context
			var capturedPod, capturedNamespace, capturedPrefix string
			testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx := r.Context()
				capturedPod, capturedNamespace, capturedPrefix = GetPodInfoFromContext(ctx)
				w.WriteHeader(http.StatusOK)
			})

			// Wrap with label middleware
			handler := server.labelMiddleware(testHandler)

			// Create request
			req := httptest.NewRequest("POST", tt.url, nil)
			rec := httptest.NewRecorder()

			// Process the request
			handler.ServeHTTP(rec, req)

			// Verify the context values were properly set
			assert.Equal(t, tt.expectedPodName, capturedPod)
			assert.Equal(t, tt.expectedNamespace, capturedNamespace)
			assert.Equal(t, tt.expectedLabelPrefix, capturedPrefix)
			assert.Equal(t, http.StatusOK, rec.Code)
		})
	}
}

func TestGetPodInfoFromContext(t *testing.T) {
	tests := []struct {
		name               string
		contextSetup       func() context.Context
		expectedPodName    string
		expectedNamespace  string
		expectedLabelPrefix string
	}{
		{
			name: "context with all values",
			contextSetup: func() context.Context {
				ctx := context.Background()
				ctx = context.WithValue(ctx, PodNameKey, "test-pod")
				ctx = context.WithValue(ctx, NamespaceKey, "test-ns")
				ctx = context.WithValue(ctx, LabelPrefix, "app.kubernetes.io/")
				return ctx
			},
			expectedPodName:    "test-pod",
			expectedNamespace:  "test-ns",
			expectedLabelPrefix: "app.kubernetes.io/",
		},
		{
			name: "context with some values",
			contextSetup: func() context.Context {
				ctx := context.Background()
				ctx = context.WithValue(ctx, PodNameKey, "test-pod")
				return ctx
			},
			expectedPodName:    "test-pod",
			expectedNamespace:  "",
			expectedLabelPrefix: "",
		},
		{
			name: "empty context",
			contextSetup: func() context.Context {
				return context.Background()
			},
			expectedPodName:    "",
			expectedNamespace:  "",
			expectedLabelPrefix: "",
		},
		{
			name: "context with non-string values",
			contextSetup: func() context.Context {
				ctx := context.Background()
				// Type mismatch should be handled gracefully
				ctx = context.WithValue(ctx, PodNameKey, 123)
				ctx = context.WithValue(ctx, NamespaceKey, true)
				return ctx
			},
			expectedPodName:    "",
			expectedNamespace:  "",
			expectedLabelPrefix: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.contextSetup()
			pod, ns, prefix := GetPodInfoFromContext(ctx)

			assert.Equal(t, tt.expectedPodName, pod)
			assert.Equal(t, tt.expectedNamespace, ns)
			assert.Equal(t, tt.expectedLabelPrefix, prefix)
		})
	}
}

func TestMiddlewareChainWithLabel(t *testing.T) {
	// Create a minimal server
	server := &Server{
		logger: zerolog.Nop(),
	}

	// Test handler to verify context values
	var capturedPod, capturedNamespace, capturedPrefix string
	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPod, capturedNamespace, capturedPrefix = GetPodInfoFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	// Manually chain the middleware - label middleware should be first in chain
	// since it adds context values that might be useful for logging/metrics
	handler := server.labelMiddleware(testHandler)

	// Create test request with URL parameters
	req := httptest.NewRequest("POST", "/mutate?pod=chain-test&namespace=chain-ns&prefix=k8s-app/", nil)
	rec := httptest.NewRecorder()

	// Process the request
	handler.ServeHTTP(rec, req)

	// Verify the context values
	assert.Equal(t, "chain-test", capturedPod)
	assert.Equal(t, "chain-ns", capturedNamespace)
	assert.Equal(t, "k8s-app/", capturedPrefix)
	assert.Equal(t, http.StatusOK, rec.Code)
}