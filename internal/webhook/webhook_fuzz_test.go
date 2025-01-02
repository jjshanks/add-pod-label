package webhook

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
)

func FuzzCreatePatch(f *testing.F) {
	// Add seed corpus with string inputs that we'll use to build the pod
	f.Add("test-pod", "default", "true", "existing", "label")
	f.Add("another-pod", "kube-system", "false", "", "")
	f.Add("", "", "", "", "")

	// Run the fuzz test
	f.Fuzz(func(t *testing.T, name string, namespace string, annotationValue string, labelKey string, labelValue string) {
		// Skip invalid inputs early using k8s validation
		if labelKey != "" {
			errs := validation.IsQualifiedName(labelKey)
			if len(errs) > 0 {
				t.Skip("Invalid label key")
			}
		}
		if labelValue != "" {
			errs := validation.IsValidLabelValue(labelValue)
			if len(errs) > 0 {
				t.Skip("Invalid label value")
			}
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
	// Add existing seed corpus entries
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

	// Add seed corpus entries with various content types
	f.Add(reviewJSON, "application/json")
	f.Add(reviewJSON, "text/plain")
	f.Add(reviewJSON, "")

	f.Fuzz(func(t *testing.T, data []byte, contentType string) {
		// If contentType is empty, default to "application/json"
		if contentType == "" {
			contentType = "application/json"
		}

		// Skip invalid or single-byte inputs for non-JSON content types
		if len(data) < 2 && contentType != "application/json" {
			return
		}

		// Create test server
		ts := newTestServer(t)

		// Create test request with fuzzed data
		req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(data))

		// Set content type
		req.Header.Set("Content-Type", contentType)

		w := httptest.NewRecorder()

		// Handle request
		ts.handleMutate(w, req)

		// Verify response
		resp := w.Result()
		defer resp.Body.Close()

		// Validate response based on content type
		switch contentType {
		case "application/json":
			// Valid content type should return 200 OK or 400 Bad Request
			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest {
				t.Errorf("Unexpected status code for JSON content type: %d", resp.StatusCode)
			}
		default:
			// Other content types should return 415 Unsupported Media Type
			if resp.StatusCode != http.StatusUnsupportedMediaType {
				t.Errorf("Expected status 415 for non-JSON content type %q, got %d", contentType, resp.StatusCode)
			}
		}
	})
}
