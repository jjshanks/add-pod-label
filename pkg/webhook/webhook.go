package webhook

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	"github.com/jjshanks/pod-label-webhook/internal/config"
)

const annotationKey = "pod-label-webhook.jjshanks.github.com/add-hello-world"

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
)

func init() {
	_ = corev1.AddToScheme(runtimeScheme)
	_ = admissionv1.AddToScheme(runtimeScheme)
}

type Config struct {
	CertFile string
	KeyFile  string
	Address  string
}

type patchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

func createPatch(pod *corev1.Pod) ([]byte, error) {
	// Check annotation
	if val, ok := pod.Annotations[annotationKey]; ok {
		// If annotation is present and set to "false", don't add label
		if val == "false" {
			return json.Marshal([]patchOperation{})
		}
	}

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
		return json.Marshal([]patchOperation{{
			Op:    "add",
			Path:  "/metadata/labels",
			Value: labels,
		}})
	}

	// If labels exist, use "replace" operation
	return json.Marshal([]patchOperation{{
		Op:    "replace",
		Path:  "/metadata/labels",
		Value: labels,
	}})
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

func validateCertPaths(certFile, keyFile string) error {
	// Validate certificate file
	certInfo, err := os.Stat(certFile)
	if err != nil {
		return fmt.Errorf("certificate file error: %v", err)
	}
	if !certInfo.Mode().IsRegular() {
		return fmt.Errorf("certificate path is not a regular file")
	}

	// Validate key file
	keyInfo, err := os.Stat(keyFile)
	if err != nil {
		return fmt.Errorf("key file error: %v", err)
	}
	if !keyInfo.Mode().IsRegular() {
		return fmt.Errorf("key path is not a regular file")
	}

	// Check key file permissions
	keyMode := keyInfo.Mode()
	if keyMode.Perm()&0o077 != 0 {
		return fmt.Errorf("key file %s has excessive permissions %v, expected 0600 or more restrictive",
			keyFile, keyMode.Perm())
	}
	if keyMode.Perm() > 0o600 {
		log.Warn().Str("key_file", keyFile).Msgf("key file has permissive mode %v, recommend 0600", keyMode.Perm())
	}

	// Validate parent directories
	certDir := filepath.Dir(certFile)
	keyDir := filepath.Dir(keyFile)

	certDirInfo, err := os.Stat(certDir)
	if err != nil {
		return fmt.Errorf("certificate directory error: %v", err)
	}
	if !certDirInfo.IsDir() {
		return fmt.Errorf("certificate parent path is not a directory")
	}

	keyDirInfo, err := os.Stat(keyDir)
	if err != nil {
		return fmt.Errorf("key directory error: %v", err)
	}
	if !keyDirInfo.IsDir() {
		return fmt.Errorf("key parent path is not a directory")
	}

	return nil
}

func Run(cfg *config.Config) error {
	// Validate certificate paths
	if err := cfg.ValidateCertPaths(); err != nil {
		return fmt.Errorf("certificate validation failed: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", handleMutate)

	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      10 * time.Second,
		ReadTimeout:       10 * time.Second,
		IdleTimeout:       120 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			CipherSuites: []uint16{
				tls.TLS_AES_128_GCM_SHA256,
				tls.TLS_AES_256_GCM_SHA384,
				tls.TLS_CHACHA20_POLY1305_SHA256,
			},
			CurvePreferences: []tls.CurveID{
				tls.X25519,
				tls.CurveP384,
			},
			SessionTicketsDisabled: true,
			Renegotiation:          tls.RenegotiateNever,
			InsecureSkipVerify:     false,
			ClientAuth:             tls.VerifyClientCertIfGiven,
		},
	}

	log.Info().
		Str("address", cfg.Address).
		Str("cert_file", cfg.CertFile).
		Str("key_file", cfg.KeyFile).
		Msg("Starting webhook server")

	return server.ListenAndServeTLS(cfg.CertFile, cfg.KeyFile)
}
