// pkg/webhook/webhook.go
package webhook

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/rs/zerolog/log"
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

	// Define a strict allowlist of valid certificate paths
	allowedCertPaths = map[string]bool{
		"/etc/webhook/certs/tls.crt": true,
		"/etc/webhook/certs/tls.key": true,
	}
)

func init() {
	_ = corev1.AddToScheme(runtimeScheme)
	_ = admissionv1.AddToScheme(runtimeScheme)
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
	logger := log.With().Str("handler", "mutate").Logger()
	logger.Debug().Msg("Received request")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Error().Err(err).Msg("Error reading body")
		http.Error(w, "error reading body", http.StatusBadRequest)
		return
	}

	if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
		logger.Error().Str("content-type", contentType).Msg("Wrong content type")
		http.Error(w, "invalid Content-Type, expected 'application/json'", http.StatusUnsupportedMediaType)
		return
	}

	admissionReview := &admissionv1.AdmissionReview{}
	if _, _, err := deserializer.Decode(body, nil, admissionReview); err != nil {
		logger.Error().Err(err).Msg("Error decoding admission review")
		http.Error(w, fmt.Sprintf("error decoding: %v", err), http.StatusBadRequest)
		return
	}

	request := admissionReview.Request
	if request == nil {
		logger.Error().Msg("Admission review request is nil")
		http.Error(w, "admission review request is nil", http.StatusBadRequest)
		return
	}

	logger = logger.With().Str("uid", string(request.UID)).Logger()
	logger.Debug().Msg("Processing request")

	pod := &corev1.Pod{}
	if err := json.Unmarshal(request.Object.Raw, pod); err != nil {
		logger.Error().Err(err).Msg("Error unmarshaling pod")
		http.Error(w, fmt.Sprintf("error unmarshaling pod: %v", err), http.StatusBadRequest)
		return
	}

	patchBytes, err := createPatch(pod)
	if err != nil {
		logger.Error().Err(err).Msg("Error creating patch")
		http.Error(w, fmt.Sprintf("error creating patch: %v", err), http.StatusInternalServerError)
		return
	}

	logger.Debug().RawJSON("patch", patchBytes).Msg("Created patch")

	patchType := admissionv1.PatchTypeJSONPatch
	response := &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
		Response: &admissionv1.AdmissionResponse{
			UID:       request.UID,
			Allowed:   true,
			Patch:     patchBytes,
			PatchType: &patchType,
		},
	}

	respBytes, err := json.Marshal(response)
	if err != nil {
		logger.Error().Err(err).Msg("Error marshaling response")
		http.Error(w, fmt.Sprintf("error marshaling response: %v", err), http.StatusInternalServerError)
		return
	}

	logger.Debug().RawJSON("response", respBytes).Msg("Sending response")

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(respBytes); err != nil {
		logger.Error().Err(err).Msg("Error writing response")
	} else {
		logger.Debug().Msg("Successfully wrote response")
	}
}

func Run(address string) error {
	certPath := "/etc/webhook/certs/tls.crt"
	keyPath := "/etc/webhook/certs/tls.key"

	if !allowedCertPaths[certPath] {
		return fmt.Errorf("certificate path not in allowlist: %s", certPath)
	}
	if !allowedCertPaths[keyPath] {
		return fmt.Errorf("key path not in allowlist: %s", keyPath)
	}

	certInfo, err := os.Stat(certPath)
	if err != nil {
		return fmt.Errorf("certificate file error: %v", err)
	}
	if !certInfo.Mode().IsRegular() {
		return fmt.Errorf("certificate path is not a regular file")
	}

	keyInfo, err := os.Stat(keyPath)
	if err != nil {
		return fmt.Errorf("key file error: %v", err)
	}
	if !keyInfo.Mode().IsRegular() {
		return fmt.Errorf("key path is not a regular file")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", handleMutate)

	server := &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      10 * time.Second,
		ReadTimeout:       10 * time.Second,
		IdleTimeout:       120 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}

	log.Info().Str("address", address).Msg("Starting webhook server")
	return server.ListenAndServeTLS(certPath, keyPath)
}
