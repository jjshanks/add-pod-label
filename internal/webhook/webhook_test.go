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

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/jjshanks/pod-label-webhook/internal/config"
)

// TestServer is a helper struct for testing
type TestServer struct {
	*Server
	logs *bytes.Buffer
}

// newTestServer creates a new test server with captured logs
func newTestServer(t *testing.T) *TestServer {
	t.Helper()

	var buf bytes.Buffer
	logger := zerolog.New(&buf).With().Timestamp().Logger()

	cfg := &config.Config{
		Address:  "localhost:8443",
		CertFile: "/tmp/cert",
		KeyFile:  "/tmp/key",
		LogLevel: "debug",
	}

	server := &Server{
		logger: logger,
		config: cfg,
	}

	return &TestServer{
		Server: server,
		logs:   &buf,
	}
}

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
		contentType   string
		expectStatus  int
		expectPatch   bool
		expectLogMsg  string
		invalidReview bool
	}{
		{
			name: "valid pod without annotations",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "test", Image: "nginx"}},
				},
			},
			contentType:  "application/json",
			expectStatus: http.StatusOK,
			expectPatch:  true,
			expectLogMsg: "Successfully processed request",
		},
		{
			name: "pod with disable annotation",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						annotationKey: "false",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "test", Image: "nginx"}},
				},
			},
			contentType:  "application/json",
			expectStatus: http.StatusOK,
			expectPatch:  false,
			expectLogMsg: "Skipping label modification",
		},
		{
			name: "pod with invalid annotation value",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						annotationKey: "invalid",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "test", Image: "nginx"}},
				},
			},
			contentType:  "application/json",
			expectStatus: http.StatusOK,
			expectPatch:  true,
			expectLogMsg: "Invalid annotation value",
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
					Containers: []corev1.Container{{Name: "test", Image: "nginx"}},
				},
			},
			contentType:  "application/json",
			expectStatus: http.StatusOK,
			expectPatch:  true,
			expectLogMsg: "Successfully processed request",
		},
		{
			name: "invalid content type",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod"},
			},
			contentType:  "text/plain",
			expectStatus: http.StatusUnsupportedMediaType,
			expectPatch:  false,
			expectLogMsg: "Invalid content type",
		},
		{
			name:          "invalid admission review",
			pod:           &corev1.Pod{},
			contentType:   "application/json",
			expectStatus:  http.StatusBadRequest,
			expectPatch:   false,
			expectLogMsg:  "Decode failed",
			invalidReview: true,
		},
		{
			name: "pod with enable annotation",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Annotations: map[string]string{
						annotationKey: "true",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "test", Image: "nginx"}},
				},
			},
			contentType:  "application/json",
			expectStatus: http.StatusOK,
			expectPatch:  true,
			expectLogMsg: "Successfully processed request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestServer(t)

			var body []byte
			if tt.invalidReview {
				body = []byte(`invalid json`)
			} else {
				ar, err := createAdmissionReview(tt.pod)
				if err != nil {
					t.Fatalf("failed to create admission review: %v", err)
				}
				body, err = json.Marshal(ar)
				if err != nil {
					t.Fatalf("failed to marshal admission review: %v", err)
				}
			}

			req := httptest.NewRequest("POST", "/mutate", bytes.NewReader(body))
			req.Header.Set("Content-Type", tt.contentType)
			req.Header.Set("X-Request-ID", "test-request-id")

			rr := httptest.NewRecorder()
			ts.handleMutate(rr, req)

			assert.Equal(t, tt.expectStatus, rr.Code)

			logs := ts.logs.String()
			assert.Contains(t, logs, tt.expectLogMsg)
			assert.Contains(t, logs, "test-request-id")

			if tt.expectStatus == http.StatusOK {
				response := &admissionv1.AdmissionReview{}
				err := json.Unmarshal(rr.Body.Bytes(), response)
				assert.NoError(t, err)

				if tt.expectPatch {
					assert.NotEmpty(t, response.Response.Patch)
					// Verify patch contains hello=world label
					var patch []map[string]interface{}
					err := json.Unmarshal(response.Response.Patch, &patch)
					assert.NoError(t, err)
					assert.True(t, containsHelloLabel(patch))

					// If pod had existing labels, verify they are preserved
					if tt.pod.Labels != nil {
						for k, v := range tt.pod.Labels {
							assert.True(t, containsLabel(patch, k, v))
						}
					}
				} else {
					// When skipping labels, we expect an empty patch array that serializes to "[]"
					assert.Equal(t, "[]", string(response.Response.Patch))
				}
			}
		})
	}
}

func containsHelloLabel(patch []map[string]interface{}) bool {
	for _, op := range patch {
		if labels, ok := op["value"].(map[string]interface{}); ok {
			if val, ok := labels["hello"]; ok && val == "world" {
				return true
			}
		}
	}
	return false
}

func containsLabel(patch []map[string]interface{}, key, value string) bool {
	for _, op := range patch {
		if labels, ok := op["value"].(map[string]interface{}); ok {
			if val, ok := labels[key]; ok && val == value {
				return true
			}
		}
	}
	return false
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
		name       string
		certFile   string
		keyFile    string
		keyMode    os.FileMode
		expectErr  bool
		errMsg     string
		expectLogs []string
	}{
		{
			name:      "valid paths and permissions",
			certFile:  certPath,
			keyFile:   keyPath,
			keyMode:   0o600,
			expectErr: false,
			expectLogs: []string{
				"Validating certificate paths",
				"Certificate paths validated successfully",
			},
		},
		{
			name:      "invalid cert path",
			certFile:  "/nonexistent/cert",
			keyFile:   keyPath,
			keyMode:   0o600,
			expectErr: true,
			errMsg:    "certificate file error",
			expectLogs: []string{
				"Validating certificate paths",
				"Certificate validation failed",
			},
		},
		{
			name:      "invalid key path",
			certFile:  certPath,
			keyFile:   "/nonexistent/key",
			keyMode:   0o600,
			expectErr: true,
			errMsg:    "key file error",
			expectLogs: []string{
				"Validating certificate paths",
				"Certificate validation failed",
			},
		},
		{
			name:      "key too permissive (world readable)",
			certFile:  certPath,
			keyFile:   keyPath,
			keyMode:   0o644,
			expectErr: true,
			errMsg:    "has excessive permissions",
			expectLogs: []string{
				"Validating certificate paths",
				"Certificate validation failed",
				"Key file has excessive permissions",
			},
		},
		{
			name:      "key too permissive (group readable)",
			certFile:  certPath,
			keyFile:   keyPath,
			keyMode:   0o640,
			expectErr: true,
			errMsg:    "has excessive permissions",
			expectLogs: []string{
				"Validating certificate paths",
				"Certificate validation failed",
				"Key file has excessive permissions",
			},
		},
		{
			name:      "key minimally permissive",
			certFile:  certPath,
			keyFile:   keyPath,
			keyMode:   0o600,
			expectErr: false,
			expectLogs: []string{
				"Validating certificate paths",
				"Certificate paths validated successfully",
			},
		},
		{
			name:      "key more restrictive",
			certFile:  certPath,
			keyFile:   keyPath,
			keyMode:   0o400,
			expectErr: false,
			expectLogs: []string{
				"Validating certificate paths",
				"Certificate paths validated successfully",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server with captured logs
			ts := newTestServer(t)

			if tt.keyFile == keyPath {
				if err := os.Chmod(keyPath, tt.keyMode); err != nil {
					t.Fatalf("failed to chmod key file: %v", err)
				}
			}

			err := ts.validateCertPaths(tt.certFile, tt.keyFile)

			// Verify error conditions
			if tt.expectErr {
				if err == nil {
					t.Error("expected error but got none")
				} else if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q but got %v", tt.errMsg, err)
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			// Verify logs
			logs := ts.logs.String()
			for _, expectedLog := range tt.expectLogs {
				if !strings.Contains(logs, expectedLog) {
					t.Errorf("expected log message %q not found in logs:\n%s", expectedLog, logs)
				}
			}

			// For non-error cases, verify log level appropriateness
			if !tt.expectErr {
				if strings.Contains(logs, "error") {
					t.Error("found error level log message in successful test case")
				}
			}
		})
	}
}

func TestCreatePatch(t *testing.T) {
	tests := []struct {
		name        string
		pod         *corev1.Pod
		expectError bool
		expectLabel bool
	}{
		{
			name: "pod without labels",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
				},
			},
			expectError: false,
			expectLabel: true,
		},
		{
			name: "pod with existing labels",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Labels: map[string]string{
						"existing": "label",
					},
				},
			},
			expectError: false,
			expectLabel: true,
		},
		{
			name: "pod with annotation to skip",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						annotationKey: "false",
					},
				},
			},
			expectError: false,
			expectLabel: false,
		},
		{
			name:        "nil pod",
			pod:         nil,
			expectError: true,
			expectLabel: false,
		},
		{
			name: "pod with invalid label key",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
					Labels: map[string]string{
						"": "invalid",
					},
				},
			},
			expectError: true,
			expectLabel: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := newTestServer(t)
			patch, err := ts.createPatch(tt.pod)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			if tt.expectLabel {
				var patchOps []map[string]interface{}
				err := json.Unmarshal(patch, &patchOps)
				assert.NoError(t, err)
				assert.True(t, containsHelloLabel(patchOps))

				// If pod had existing labels, verify they are preserved
				if tt.pod.Labels != nil {
					for k, v := range tt.pod.Labels {
						assert.True(t, containsLabel(patchOps, k, v))
					}
				}
			} else {
				// When skipping labels, we expect an empty patch array that serializes to "[]"
				assert.Equal(t, "[]", string(patch))
			}
		})
	}
}
