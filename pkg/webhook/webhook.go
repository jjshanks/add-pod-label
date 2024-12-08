package webhook

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()

	// Define the base directory for certificates
	certDir  = "/etc/webhook/certs"
	certFile = "tls.crt"
	keyFile  = "tls.key"
)

func init() {
	_ = corev1.AddToScheme(runtimeScheme)
	_ = admissionv1.AddToScheme(runtimeScheme)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.LUTC | log.Lshortfile)
}

func validateAndResolvePath(base, file string) (string, error) {
	// Clean the paths
	base = filepath.Clean(base)
	file = filepath.Clean(file)

	// Check if file contains any directory traversal attempts
	if strings.Contains(file, "..") {
		return "", fmt.Errorf("invalid file path: directory traversal detected")
	}

	// Join paths and verify it's still under base directory
	fullPath := filepath.Join(base, file)
	if !strings.HasPrefix(fullPath, base) {
		return "", fmt.Errorf("invalid file path: outside of base directory")
	}

	// Verify the file exists and is a regular file
	info, err := os.Stat(fullPath)
	if err != nil {
		return "", fmt.Errorf("error accessing file: %v", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("path is a directory, expected a file")
	}

	return fullPath, nil
}

type patchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

func createPatch(pod *corev1.Pod) ([]byte, error) {
	var patch []patchOperation

	// Create a new labels map that includes both existing labels and our new label
	labels := make(map[string]string)
	if pod.Labels != nil {
		for k, v := range pod.Labels {
			labels[k] = v
		}
	}
	labels["hello"] = "world"

	// If there are no existing labels, use "add" operation
	if pod.Labels == nil {
		patch = []patchOperation{{
			Op:    "add",
			Path:  "/metadata/labels",
			Value: labels,
		}}
	} else {
		// If labels exist, use "replace" operation
		patch = []patchOperation{{
			Op:    "replace",
			Path:  "/metadata/labels",
			Value: labels,
		}}
	}

	return json.Marshal(patch)
}

func handleMutate(w http.ResponseWriter, r *http.Request) {
	log.Printf("Received request")

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading body: %v", err)
		http.Error(w, "error reading body", http.StatusBadRequest)
		return
	}

	// Verify content type
	if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
		log.Printf("Wrong content type: %s", contentType)
		http.Error(w, "invalid Content-Type, expected 'application/json'", http.StatusUnsupportedMediaType)
		return
	}

	// Parse the AdmissionReview
	admissionReview := &admissionv1.AdmissionReview{}
	if _, _, err := deserializer.Decode(body, nil, admissionReview); err != nil {
		log.Printf("Error decoding admission review: %v", err)
		http.Error(w, fmt.Sprintf("error decoding: %v", err), http.StatusBadRequest)
		return
	}

	// Ensure we have a request
	request := admissionReview.Request
	if request == nil {
		log.Printf("Admission review request is nil")
		http.Error(w, "admission review request is nil", http.StatusBadRequest)
		return
	}

	log.Printf("Processing request for UID: %s", request.UID)

	// Parse the Pod
	pod := &corev1.Pod{}
	if err := json.Unmarshal(request.Object.Raw, pod); err != nil {
		log.Printf("Error unmarshaling pod: %v", err)
		http.Error(w, fmt.Sprintf("error unmarshaling pod: %v", err), http.StatusBadRequest)
		return
	}

	patchBytes, err := createPatch(pod)
	if err != nil {
		log.Printf("Error creating patch: %v", err)
		http.Error(w, fmt.Sprintf("error creating patch: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Created patch: %s", string(patchBytes))

	// Create the admission response
	patchType := admissionv1.PatchTypeJSONPatch
	admissionResponse := &admissionv1.AdmissionResponse{
		UID:       request.UID,
		Allowed:   true,
		Patch:     patchBytes,
		PatchType: &patchType,
	}

	// Create the admission review response
	response := &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Response: admissionResponse,
	}

	respBytes, err := json.Marshal(response)
	if err != nil {
		log.Printf("Error marshaling response: %v", err)
		http.Error(w, fmt.Sprintf("error marshaling response: %v", err), http.StatusInternalServerError)
		return
	}

	log.Printf("Sending response for UID: %s", request.UID)
	log.Printf("Response JSON: %s", string(respBytes))

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(respBytes); err != nil {
		log.Printf("Error writing response: %v", err)
	} else {
		log.Printf("Successfully wrote response")
	}
}

func isValidPath(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func Run() error {
	// Validate and resolve certificate paths
	certPath, err := validateAndResolvePath(certDir, certFile)
	if err != nil {
		return fmt.Errorf("invalid certificate file: %v", err)
	}

	keyPath, err := validateAndResolvePath(certDir, keyFile)
	if err != nil {
		return fmt.Errorf("invalid key file: %v", err)
	}

	// Read and validate certificates
	if !isValidPath(certPath) {
		return fmt.Errorf("invalid certificate path: %s", certPath)
	}
	if !isValidPath(keyPath) {
		return fmt.Errorf("invalid key path: %s", keyPath)
	}

	if _, err := os.ReadFile(certPath); err != nil {
		return fmt.Errorf("failed to read certificate file: %v", err)
	}
	if _, err := os.ReadFile(keyPath); err != nil {
		return fmt.Errorf("failed to read key file: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", handleMutate)

	server := &http.Server{
		Addr:              ":8443",
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      10 * time.Second,
		ReadTimeout:       10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	log.Printf("Starting webhook server on :8443")
	return server.ListenAndServeTLS(certPath, keyPath)
}
