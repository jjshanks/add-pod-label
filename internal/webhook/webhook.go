// webhook.go
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
	if pod == nil {
		return nil, &WebhookError{
			Op:  "validate",
			Err: fmt.Errorf("pod is nil"),
		}
	}

	s.logger.Debug().
		Str("pod", pod.Name).
		Str("namespace", pod.Namespace).
		Bool("has_labels", pod.Labels != nil).
		Bool("has_hello_annotation", pod.Annotations != nil && pod.Annotations[annotationKey] != "").
		Msg("Creating patch")

	// Record annotation validation result
	if pod.Annotations == nil {
		s.metrics.recordAnnotationValidation(annotationMissing, pod.Namespace)
	} else if val, ok := pod.Annotations[annotationKey]; ok {
		if _, err := strconv.ParseBool(val); err != nil {
			s.metrics.recordAnnotationValidation(annotationInvalid, pod.Namespace)
		} else {
			s.metrics.recordAnnotationValidation(annotationValid, pod.Namespace)
		}
	} else {
		s.metrics.recordAnnotationValidation(annotationMissing, pod.Namespace)
	}

	// Check if labels should be added based on annotation
	if !s.shouldAddLabel(pod) {
		s.logger.Debug().
			Str("pod", pod.Name).
			Msg("Skipping label modification due to annotation")

		s.metrics.recordLabelOperation(labelOperationSkipped, pod.Namespace)
		// Return an empty patch array to indicate no changes
		return json.Marshal([]patchOperation{})
	}

	// Create labels map with validation
	labels := make(map[string]string)
	if pod.Labels != nil {
		for k, v := range pod.Labels {
			if k == "" {
				s.metrics.recordLabelOperation(labelOperationError, pod.Namespace)
				return nil, newValidationError(
					fmt.Errorf("empty label key found"),
					fmt.Sprintf("pod/%s", pod.Name),
				)
			}
			labels[k] = v
		}
	}
	labels["hello"] = "world"

	// Create patch operations with validation
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

	// Marshal patch with error context
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		s.metrics.recordLabelOperation(labelOperationError, pod.Namespace)
		return nil, newPatchError(
			fmt.Errorf("failed to marshal patch: %w", err),
			fmt.Sprintf("pod/%s", pod.Name),
		)
	}

	s.logger.Debug().
		Str("pod", pod.Name).
		Int("label_count", len(labels)).
		Msg("Successfully created label patch")

	s.metrics.recordLabelOperation(labelOperationSuccess, pod.Namespace)
	return patchBytes, nil
}

func (s *Server) handleMutate(w http.ResponseWriter, r *http.Request) {
	// Get request ID for error context
	reqID := r.Header.Get("X-Request-ID")
	if reqID == "" {
		reqID = uuid.New().String()
	}

	logger := s.logger.With().
		Str("request_id", reqID).
		Str("handler", "mutate").
		Logger()

	// Read request body with error context
	body, err := io.ReadAll(r.Body)
	if err != nil {
		err = fmt.Errorf("failed to read request body: %w", err)
		logger.Error().Err(err).Msg("Request read failed")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Validate content type with specific error
	if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
		err := fmt.Errorf("invalid Content-Type %q, expected 'application/json'", contentType)
		logger.Error().Err(err).Str("content_type", contentType).Msg("Invalid content type")
		http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
		return
	}

	// Decode admission review with structured error
	admissionReview := &admissionv1.AdmissionReview{}
	if _, _, err := deserializer.Decode(body, nil, admissionReview); err != nil {
		err = newDecodeError(err, "admission review")
		logger.Error().Err(err).Msg("Decode failed")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	request := admissionReview.Request
	if request == nil {
		err := &WebhookError{
			Op:  "validate",
			Err: fmt.Errorf("admission review request is nil"),
		}
		logger.Error().Err(err).Msg("Validation failed")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Add request UID to logger context
	logger = logger.With().Str("uid", string(request.UID)).Logger()

	// Unmarshal pod with context
	pod := &corev1.Pod{}
	if err := json.Unmarshal(request.Object.Raw, pod); err != nil {
		err = newDecodeError(err, fmt.Sprintf("pod/%s", pod.Name))
		logger.Error().Err(err).Msg("Pod unmarshal failed")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Create patch with error wrapping
	patchBytes, err := s.createPatch(pod)
	if err != nil {
		err = newPatchError(err, fmt.Sprintf("pod/%s", pod.Name))
		logger.Error().Err(err).Msg("Patch creation failed")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Build and send response with error handling
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
		err = fmt.Errorf("failed to marshal response: %w", err)
		logger.Error().Err(err).Msg("Response marshal failed")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(respBytes); err != nil {
		logger.Error().Err(err).Msg("Failed to write response")
		return
	}

	logger.Debug().Msg("Successfully processed request")
}
