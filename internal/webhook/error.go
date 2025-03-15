// Package webhook provides functionality for webhook operations.
// This file defines the custom error types and error handling utilities.
package webhook

import (
	"fmt"
)

// Error represents an error that occurred during webhook processing.
// It provides context about the operation that failed and the resource being processed.
// The error implements the error interface and supports error wrapping for
// maintaining error chains.
type Error struct {
	Op   string // The operation that failed (e.g., "decode", "validate", "patch")
	Path string // Resource path or identifier (e.g., "pod/my-pod")
	Err  error  // The underlying error that caused the failure
}

// Error implements the error interface.
// It formats the error message to include the operation, resource path (if any),
// and underlying error details.
func (e *Error) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("webhook %s failed for %s: %v", e.Op, e.Path, e.Err)
	}
	return fmt.Sprintf("webhook %s failed: %v", e.Op, e.Err)
}

// Unwrap implements the error unwrapping interface.
// It returns the underlying error to support error chains and
// work with errors.Is and errors.As.
func (e *Error) Unwrap() error {
	return e.Err
}

// Error constructors
// These functions create specific types of webhook errors with
// appropriate context and consistent formatting.

// newDecodeError creates an error for JSON decoding failures.
// Used when unmarshaling admission reviews or pod specifications fails.
func newDecodeError(err error, resourcePath string) error {
	return &Error{
		Op:   "decode",
		Path: resourcePath,
		Err:  err,
	}
}

// newValidationError creates an error for validation failures.
// Used when pod specifications or configurations fail validation checks.
func newValidationError(err error, resourcePath string) error {
	return &Error{
		Op:   "validate",
		Path: resourcePath,
		Err:  err,
	}
}

// newPatchError creates an error for patch creation or application failures.
// Used when generating or applying JSON patches to pods fails.
func newPatchError(err error, resourcePath string) error {
	return &Error{
		Op:   "patch",
		Path: resourcePath,
		Err:  err,
	}
}
