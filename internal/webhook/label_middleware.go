package webhook

import (
	"context"
	"net/http"
)

// LabelContextKey is the key used to store labels in request context
type LabelContextKey string

const (
	// PodNameKey is the context key for pod name
	PodNameKey LabelContextKey = "pod_name"
	// NamespaceKey is the context key for namespace
	NamespaceKey LabelContextKey = "namespace"
	// LabelPrefix is the prefix for added labels
	LabelPrefix LabelContextKey = "label_prefix"
)

// labelMiddleware adds pod-specific context to incoming requests
// to enable better debugging, metrics and tracing.
//
// This middleware:
// - Extracts pod name and namespace from request path or headers
// - Adds information to the request context
// - Enables downstream handlers to access pod context
// - Passes standard headers through to downstream handlers
func (s *Server) labelMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract pod and namespace information from request headers or URL parameters
		podName := r.URL.Query().Get("pod")
		namespace := r.URL.Query().Get("namespace") 
		labelPrefix := r.URL.Query().Get("prefix")

		// Create new context with label information
		ctx := r.Context()
		if podName != "" {
			ctx = context.WithValue(ctx, PodNameKey, podName)
		}
		if namespace != "" {
			ctx = context.WithValue(ctx, NamespaceKey, namespace)
		}
		if labelPrefix != "" {
			ctx = context.WithValue(ctx, LabelPrefix, labelPrefix)
		}

		// Pass the enriched context to the next handler
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// GetPodInfoFromContext returns pod information from the request context
func GetPodInfoFromContext(ctx context.Context) (podName, namespace, prefix string) {
	if name, ok := ctx.Value(PodNameKey).(string); ok {
		podName = name
	}
	if ns, ok := ctx.Value(NamespaceKey).(string); ok {
		namespace = ns
	}
	if pre, ok := ctx.Value(LabelPrefix).(string); ok {
		prefix = pre
	}
	return
}