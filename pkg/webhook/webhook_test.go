//go:build !integration
// +build !integration

package webhook

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func createAdmissionReview(pod *corev1.Pod) (*admissionv1.AdmissionReview, error) {
	raw, err := json.Marshal(pod)
	if err != nil {
		return nil, err
	}

	return &admissionv1.AdmissionReview{
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
			Name:      pod.Name,
			Namespace: pod.Namespace,
			Operation: admissionv1.Create,
			Object: runtime.RawExtension{
				Raw: raw,
			},
		},
	}, nil
}

func TestHandleMutate(t *testing.T) {
	tests := []struct {
		name          string
		pod           *corev1.Pod
		expectError   bool
		expectedLabel string
	}{
		{
			name: "pod with no labels",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx",
						},
					},
				},
			},
			expectError:   false,
			expectedLabel: "world",
		},
		{
			name: "pod with existing labels",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels: map[string]string{
						"existing": "label",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx",
						},
					},
				},
			},
			expectError:   false,
			expectedLabel: "world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create admission review
			ar, err := createAdmissionReview(tt.pod)
			if err != nil {
				t.Fatalf("failed to create admission review: %v", err)
			}

			// Marshal admission review
			body, err := json.Marshal(ar)
			if err != nil {
				t.Fatalf("failed to marshal admission review: %v", err)
			}

			// Create request
			req := httptest.NewRequest("POST", "/mutate", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			// Create response recorder
			rr := httptest.NewRecorder()

			// Call handler
			handleMutate(rr, req)

			// Check response
			if rr.Code != http.StatusOK && !tt.expectError {
				t.Errorf("handler returned wrong status code: got %v want %v",
					rr.Code, http.StatusOK)
			}

			// Parse response
			response := &admissionv1.AdmissionReview{}
			if err := json.Unmarshal(rr.Body.Bytes(), response); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			// Verify response
			if response.Response.UID != ar.Request.UID {
				t.Errorf("handler returned wrong UID: got %v want %v",
					response.Response.UID, ar.Request.UID)
			}

			if !response.Response.Allowed {
				t.Error("handler returned not allowed")
			}

			// Verify patch
			var patch []map[string]interface{}
			if err := json.Unmarshal(response.Response.Patch, &patch); err != nil {
				t.Fatalf("failed to unmarshal patch: %v", err)
			}

			// Check that hello=world label is in the patch
			found := false
			for _, p := range patch {
				if p["op"] == "add" || p["op"] == "replace" {
					if labels, ok := p["value"].(map[string]interface{}); ok {
						if val, ok := labels["hello"]; ok && val == tt.expectedLabel {
							found = true
							break
						}
					}
				}
			}
			if !found {
				t.Error("patch does not contain expected label")
			}
		})
	}
}

func TestCreatePatch(t *testing.T) {
	tests := []struct {
		name        string
		pod         *corev1.Pod
		expectError bool
		expectOp    string
	}{
		{
			name: "pod with no labels",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{},
			},
			expectError: false,
			expectOp:    "add",
		},
		{
			name: "pod with existing labels",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"existing": "label",
					},
				},
			},
			expectError: false,
			expectOp:    "replace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patch, err := createPatch(tt.pod)
			if (err != nil) != tt.expectError {
				t.Errorf("createPatch() error = %v, expectError %v", err, tt.expectError)
				return
			}

			var patchOps []map[string]interface{}
			if err := json.Unmarshal(patch, &patchOps); err != nil {
				t.Fatalf("failed to unmarshal patch: %v", err)
			}

			if len(patchOps) != 1 {
				t.Fatalf("expected 1 patch operation, got %d", len(patchOps))
			}

			if patchOps[0]["op"] != tt.expectOp {
				t.Errorf("expected operation %s, got %s", tt.expectOp, patchOps[0]["op"])
			}

			labels, ok := patchOps[0]["value"].(map[string]interface{})
			if !ok {
				t.Fatal("patch value is not a map")
			}

			if labels["hello"] != "world" {
				t.Error("patch does not contain hello=world label")
			}

			if tt.pod.Labels != nil {
				for k, v := range tt.pod.Labels {
					if labels[k] != v {
						t.Errorf("patch is missing existing label %s=%s", k, v)
					}
				}
			}
		})
	}
}

func TestValidateCertPaths(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "webhook-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files
	certPath := filepath.Join(tmpDir, "tls.crt")
	keyPath := filepath.Join(tmpDir, "tls.key")

	if err := os.WriteFile(certPath, []byte("test-cert"), 0o644); err != nil {
		t.Fatalf("failed to create test cert: %v", err)
	}
	if err := os.WriteFile(keyPath, []byte("test-key"), 0o600); err != nil {
		t.Fatalf("failed to create test key: %v", err)
	}

	tests := []struct {
		name      string
		certFile  string
		keyFile   string
		keyMode   os.FileMode
		expectErr bool
		errMsg    string
	}{
		{
			name:      "valid paths and permissions",
			certFile:  certPath,
			keyFile:   keyPath,
			keyMode:   0o600,
			expectErr: false,
		},
		{
			name:      "invalid cert path",
			certFile:  "/nonexistent/cert",
			keyFile:   keyPath,
			keyMode:   0o600,
			expectErr: true,
			errMsg:    "certificate file error",
		},
		{
			name:      "invalid key path",
			certFile:  certPath,
			keyFile:   "/nonexistent/key",
			keyMode:   0o600,
			expectErr: true,
			errMsg:    "key file error",
		},
		{
			name:      "key too permissive (world readable)",
			certFile:  certPath,
			keyFile:   keyPath,
			keyMode:   0o644,
			expectErr: true,
			errMsg:    "has excessive permissions",
		},
		{
			name:      "key too permissive (group readable)",
			certFile:  certPath,
			keyFile:   keyPath,
			keyMode:   0o640,
			expectErr: true,
			errMsg:    "has excessive permissions",
		},
		{
			name:      "key minimally permissive",
			certFile:  certPath,
			keyFile:   keyPath,
			keyMode:   0o600,
			expectErr: false,
		},
		{
			name:      "key more restrictive",
			certFile:  certPath,
			keyFile:   keyPath,
			keyMode:   0o400,
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.keyFile == keyPath {
				if err := os.Chmod(keyPath, tt.keyMode); err != nil {
					t.Fatalf("failed to chmod key file: %v", err)
				}
			}

			err := validateCertPaths(tt.certFile, tt.keyFile)
			if tt.expectErr {
				if err == nil {
					t.Error("expected error but got none")
				} else if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q but got %v", tt.errMsg, err)
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
