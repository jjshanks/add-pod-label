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

// recordAnnotationMetrics records metrics related to annotation validation
func (s *Server) recordAnnotationMetrics(pod *corev1.Pod) {
	if pod.Annotations == nil {
		s.metrics.recordAnnotationValidation(annotationMissing, pod.Namespace)
		return
	}

	if val, ok := pod.Annotations[annotationKey]; ok {
		if _, err := strconv.ParseBool(val); err != nil {
			s.metrics.recordAnnotationValidation(annotationInvalid, pod.Namespace)
		} else {
			s.metrics.recordAnnotationValidation(annotationValid, pod.Namespace)
		}
	} else {
		s.metrics.recordAnnotationValidation(annotationMissing, pod.Namespace)
	}
}

// shouldAddLabel determines whether a label should be added to a pod
// based on its annotations. The default behavior is to add the label.
//
// Rules:
// - If no annotation is present, return true (add label)
// - If annotation is "true", return true (add label)
// - If annotation is "false", return false (skip label)
// - If annotation has an invalid value, log a warning and default to true
func (s *Server) shouldAddLabel(pod *corev1.Pod) bool {
	// Retrieve the annotation value
	val, ok := pod.Annotations[annotationKey]
	if !ok {
		// No annotation present, default to adding label
		return true
	}

	// Attempt to parse the annotation value as a boolean
	parsed, err := strconv.ParseBool(val)
	if err != nil {
		// Invalid annotation value: log warning and default to true
		s.logger.Warn().
			Str("value", val).
			Str("pod", pod.Name).
			Str("namespace", pod.Namespace).
			Msg("Invalid annotation value, defaulting to true")
		return true
	}

	return parsed
}

// createLabelsMap creates a map of labels, including existing ones and the hello label
func (s *Server) createLabelsMap(pod *corev1.Pod) (map[string]string, error) {
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
	return labels, nil
}

// createPatch generates a JSON patch for modifying pod labels
//
// This method handles several scenarios:
// 1. Pods without any existing labels
// 2. Pods with existing labels
// 3. Pods with annotation to disable labeling
//
// Returns:
// - A JSON patch that can add or replace labels
// - An error if validation fails (e.g., nil pod, invalid label key)
func (s *Server) createPatch(pod *corev1.Pod) ([]byte, error) {
	// Validate input pod
	if pod == nil {
		return nil, &WebhookError{
			Op:  "validate",
			Err: fmt.Errorf("pod is nil"),
		}
	}

	// Log detailed information about the pod for debugging and audit purposes
	s.logger.Debug().
		Str("pod", pod.Name).
		Str("namespace", pod.Namespace).
		Bool("has_existing_labels", pod.Labels != nil).
		Bool("has_hello_annotation", pod.Annotations != nil && pod.Annotations[annotationKey] != "").
		Msg("Preparing to create label patch")

	s.recordAnnotationMetrics(pod)

	// Check if labels should be added based on annotation
	if !s.shouldAddLabel(pod) {
		s.logger.Debug().
			Str("pod", pod.Name).
			Msg("Skipping label modification due to annotation")
		s.metrics.recordLabelOperation(labelOperationSkipped, pod.Namespace)
		return json.Marshal([]patchOperation{})
	}

	labels, err := s.createLabelsMap(pod)
	if err != nil {
		return nil, err
	}

	// Prepare patch operations based on whether labels exist
	var patch []patchOperation
	if pod.Labels == nil {
		// If no labels exist, add a new labels map
		patch = []patchOperation{{
			Op:    "add",
			Path:  "/metadata/labels",
			Value: labels,
		}}
	} else {
		// If labels exist, replace the entire labels map
		patch = []patchOperation{{
			Op:    "replace",
			Path:  "/metadata/labels",
			Value: labels,
		}}
	}

	// Marshal patch with error handling
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

// handleMutate is the HTTP handler for the mutating webhook
//
// This method:
// 1. Validates the incoming request
// 2. Extracts the pod from the admission review
// 3. Generates a patch to modify pod labels
// 4. Sends an admission review response
//
// Handles various error scenarios and provides detailed logging
func (s *Server) handleMutate(w http.ResponseWriter, r *http.Request) {
	// Generate a unique request ID for tracing
	reqID := r.Header.Get("X-Request-ID")
	if reqID == "" {
		reqID = uuid.New().String()
	}

	// Create a context-aware logger with request details
	logger := s.logger.With().
		Str("request_id", reqID).
		Str("handler", "mutate").
		Logger()

	// Read the entire request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		err = fmt.Errorf("failed to read request body: %w", err)
		logger.Error().Err(err).Msg("Request read failed")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Validate content type
	if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
		err := fmt.Errorf("invalid Content-Type %q, expected 'application/json'", contentType)
		logger.Error().Err(err).Str("content_type", contentType).Msg("Invalid content type")
		http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
		return
	}

	// Decode the admission review
	admissionReview := &admissionv1.AdmissionReview{}
	if _, _, err := deserializer.Decode(body, nil, admissionReview); err != nil {
		err = newDecodeError(err, "admission review")
		logger.Error().Err(err).Msg("Decode failed")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Validate admission review request
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

	// Unmarshal the pod from the request
	pod := &corev1.Pod{}
	if err := json.Unmarshal(request.Object.Raw, pod); err != nil {
		err = newDecodeError(err, fmt.Sprintf("pod/%s", pod.Name))
		logger.Error().Err(err).Msg("Pod unmarshal failed")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Create label patch
	patchBytes, err := s.createPatch(pod)
	if err != nil {
		err = newPatchError(err, fmt.Sprintf("pod/%s", pod.Name))
		logger.Error().Err(err).Msg("Patch creation failed")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Prepare admission review response
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

	// Marshal response
	respBytes, err := json.Marshal(response)
	if err != nil {
		err = fmt.Errorf("failed to marshal response: %w", err)
		logger.Error().Err(err).Msg("Response marshal failed")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Write response
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(respBytes); err != nil {
		logger.Error().Err(err).Msg("Failed to write response")
		return
	}

	logger.Debug().Msg("Successfully processed request")
}
