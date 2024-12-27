package webhook

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
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

type patchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

func (s *Server) shouldAddLabel(pod *corev1.Pod) bool {
	val, ok := pod.Annotations[annotationKey]
	if !ok {
		return true
	}

	parsed, err := strconv.ParseBool(val)
	if err != nil {
		s.logger.Warn().
			Str("value", val).
			Str("pod", pod.Name).
			Str("namespace", pod.Namespace).
			Msg("Invalid annotation value, defaulting to true")
		return true
	}

	return parsed
}

func (s *Server) createPatch(pod *corev1.Pod) ([]byte, error) {
	s.logger.Debug().
		Str("pod", pod.Name).
		Str("namespace", pod.Namespace).
		Interface("labels", pod.Labels).
		Interface("annotations", pod.Annotations).
		Msg("Creating patch for pod")

	if !s.shouldAddLabel(pod) {
		s.logger.Debug().Str("pod", pod.Name).Msg("Skipping label modification due to annotation")
		return json.Marshal([]patchOperation{})
	}

	// Create a new labels map that includes both existing labels and our new label
	labels := make(map[string]string)
	if pod.Labels != nil {
		for k, v := range pod.Labels {
			labels[k] = v
		}
	}
	labels["hello"] = "world"

	var patch []patchOperation
	if pod.Labels == nil {
		patch = []patchOperation{{
			Op:    "add",
			Path:  "/metadata/labels",
			Value: labels,
		}}
	} else {
		patch = []patchOperation{{
			Op:    "replace",
			Path:  "/metadata/labels",
			Value: labels,
		}}
	}

	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal patch: %w", err)
	}

	s.logger.Debug().Msg("Successfully created patch")
	return patchBytes, nil
}

func (s *Server) handleMutate(w http.ResponseWriter, r *http.Request) {
	// Create request-scoped logger with request ID
	reqID := r.Header.Get("X-Request-ID")
	if reqID == "" {
		reqID = uuid.New().String()
	}

	logger := s.logger.With().
		Str("request_id", reqID).
		Str("handler", "mutate").
		Logger()

	logger.Debug().
		Str("method", r.Method).
		Str("url", r.URL.String()).
		Str("remote_addr", r.RemoteAddr).
		Msg("Received request")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to read request body")
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

	patchBytes, err := s.createPatch(pod)
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
