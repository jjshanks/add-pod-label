package webhook

import (
	"fmt"
)

// Define custom error types for specific webhook errors
type WebhookError struct {
	Op   string // Operation that failed
	Path string // Resource path if applicable
	Err  error  // Original error
}

func (e *WebhookError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("webhook %s failed for %s: %v", e.Op, e.Path, e.Err)
	}
	return fmt.Sprintf("webhook %s failed: %v", e.Op, e.Err)
}

func (e *WebhookError) Unwrap() error {
	return e.Err
}

// Common error constructors
func newDecodeError(err error, resourcePath string) error {
	return &WebhookError{
		Op:   "decode",
		Path: resourcePath,
		Err:  err,
	}
}

func newValidationError(err error, resourcePath string) error {
	return &WebhookError{
		Op:   "validate",
		Path: resourcePath,
		Err:  err,
	}
}

func newPatchError(err error, resourcePath string) error {
	return &WebhookError{
		Op:   "patch",
		Path: resourcePath,
		Err:  err,
	}
}
