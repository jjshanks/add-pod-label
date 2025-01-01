// internal/webhook/webhook_fuzz_test.go
package webhook

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"unicode/utf8"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// isValidLabelValue checks if a string is a valid Kubernetes label value
func isValidLabelValue(s string) bool {
	if len(s) > 63 || len(s) == 0 {
		return false
	}
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

// isValidLabelKey checks if a string is a valid Kubernetes label key
func isValidLabelKey(s string) bool {
	if len(s) > 253 || len(s) == 0 {
		return false
	}
	if !utf8.ValidString(s) {
		return false
	}
	parts := split(s, '/')
	if len(parts) > 2 {
		return false
	}
	for _, part := range parts {
		if !isValidLabelValue(part) {
			return false
		}
	}
	return true
}

// split splits a string by a separator, handling empty strings correctly
func split(s string, sep rune) []string {
	var result []string
	current := ""
	for _, r := range s {
		if r == sep {
			result = append(result, current)
			current = ""
		} else {
			current += string(r)
		}
	}
	result = append(result, current)
	return result
}

func FuzzCreatePatch(f *testing.F) {
	// Add seed corpus with string inputs that we'll use to build the pod
	f.Add("test-pod", "default", "true", "existing", "label")
	f.Add("another-pod", "kube-system", "false", "", "")
	f.Add("", "", "", "", "")

	// Run the fuzz test
	f.Fuzz(func(t *testing.T, name string, namespace string, annotationValue string, labelKey string, labelValue string) {
		// Skip invalid inputs early
		if (labelKey != "" && !isValidLabelKey(labelKey)) || (labelValue != "" && !isValidLabelValue(labelValue)) {
			t.Skip("Invalid label key or value")
		}

		// Create a test server
		ts := newTestServer(t)

		// Create labels map if key is not empty
		var labels map[string]string
		if labelKey != "" {
			labels = map[string]string{labelKey: labelValue}
		}

		// Create a pod with fuzzed values
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    labels,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "test",
						Image: "test",
					},
				},
			},
		}

		// Add annotation if value is not empty
		if annotationValue != "" {
			pod.Annotations = map[string]string{
				annotationKey: annotationValue,
			}
		}

		// Try to create patch
		patch, err := ts.createPatch(pod)

		if err != nil {
			// Some combinations of input will produce valid errors
			// Make sure the error is one we expect
			if pod == nil {
				return // nil pod error is expected
			}
			if _, ok := err.(*WebhookError); !ok {
				t.Errorf("unexpected error type: %T", err)
			}
			return
		}

		// Verify patch is valid JSON
		var patchOps []map[string]interface{}
		if err := json.Unmarshal(patch, &patchOps); err != nil {
			t.Errorf("invalid patch JSON: %v", err)
			return
		}

		// Verify patch operations
		for _, op := range patchOps {
			// Check operation type
			if op["op"] != "add" && op["op"] != "replace" {
				t.Errorf("invalid operation type: %v", op["op"])
			}

			// Check path
			if op["path"] != "/metadata/labels" {
				t.Errorf("invalid path: %v", op["path"])
			}

			// Check value is a map
			if value, ok := op["value"].(map[string]interface{}); ok {
				// Verify hello label is present
				if annotationValue != "false" {
					if hello, exists := value["hello"]; !exists || hello != "world" {
						t.Errorf("missing or invalid hello label")
					}
				}

				// Verify existing labels are preserved
				if labelKey != "" {
					if value[labelKey] != labelValue {
						t.Errorf("existing label %s not preserved", labelKey)
					}
				}
			} else {
				t.Errorf("value is not a map")
			}
		}
	})
}

func FuzzHandleMutate(f *testing.F) {
	// Add seed corpus with valid admission review
	podJSON, _ := json.Marshal(&corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pod",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: "test", Image: "test"}},
		},
	})

	review := &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Request: &admissionv1.AdmissionRequest{
			UID: "test-uid",
			Kind: metav1.GroupVersionKind{
				Group:   "",
				Version: "v1",
				Kind:    "Pod",
			},
			Resource: metav1.GroupVersionResource{
				Group:    "",
				Version:  "v1",
				Resource: "pods",
			},
			Object: runtime.RawExtension{
				Raw: podJSON,
			},
		},
	}
	reviewJSON, _ := json.Marshal(review)
	f.Add(reviewJSON)

	f.Fuzz(func(t *testing.T, data []byte) {
		// Skip if data is empty or not valid UTF-8
		if len(data) == 0 || !utf8.Valid(data) {
			return
		}

		// Create test server
		ts := newTestServer(t)

		// Create test request with fuzzed data
		req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()

		// Handle request
		ts.handleMutate(w, req)

		// Verify response status code is valid
		if w.Code != http.StatusOK && w.Code != http.StatusBadRequest &&
			w.Code != http.StatusUnsupportedMediaType {
			t.Errorf("unexpected status code: %d", w.Code)
		}

		// Verify response
		resp := w.Result()
		defer resp.Body.Close()

		// Successful responses should have application/json content-type
		if resp.StatusCode == http.StatusOK {
			contentType := resp.Header.Get("Content-Type")
			if contentType != "application/json" {
				t.Errorf("successful response missing or invalid content type: %s", contentType)
			}
		}

		// Read response body
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Errorf("failed to read response body: %v", err)
			return
		}

		// For 200 responses, verify it's a valid admission review
		if resp.StatusCode == http.StatusOK {
			review := &admissionv1.AdmissionReview{}
			if err := json.Unmarshal(body, review); err != nil {
				t.Errorf("invalid response JSON: %v", err)
				return
			}

			if review.Response == nil {
				t.Error("missing response in admission review")
				return
			}

			if !review.Response.Allowed {
				t.Error("response not allowed")
			}

			// Verify patch if present
			if len(review.Response.Patch) > 0 {
				var patchOps []map[string]interface{}
				if err := json.Unmarshal(review.Response.Patch, &patchOps); err != nil {
					t.Errorf("invalid patch JSON: %v", err)
				}
			}
		}
	})
}
