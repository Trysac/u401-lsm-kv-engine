package lsmkvengine

import (
	"errors"
	"fmt"
)

// Sentinel errors for matching via errors.Is.
var (
	ErrNotFound        = &LSMError{Code: "not_found", Message: "key not found"}
	ErrCorruptWAL      = &LSMError{Code: "corrupt_wal", Message: "corrupt write-ahead log"}
	ErrCorruptSSTable  = &LSMError{Code: "corrupt_sstable", Message: "corrupt sstable"}
	ErrCorruptManifest = &LSMError{Code: "corrupt_manifest", Message: "corrupt manifest"}
	ErrClosed          = &LSMError{Code: "closed", Message: "database is closed"}
	ErrKeyTooLarge     = &LSMError{Code: "key_too_large", Message: "key exceeds maximum allowed size"}
	ErrValTooLarge     = &LSMError{Code: "val_too_large", Message: "value exceeds maximum allowed size"}
)

// LSMError represents a typed error originating from the LSM engine.
type LSMError struct {
	Code    string
	Message string
	Err     error
}

// Error implements the error interface.
func (e *LSMError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return fmt.Sprintf("lsmkvengine [%s]: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("lsmkvengine [%s]: %s", e.Code, e.Message)
}

// Unwrap returns the underlying error if any.
func (e *LSMError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Is compares the error code with the target error.
func (e *LSMError) Is(target error) bool {
	if target == nil {
		return false
	}
	t, ok := target.(*LSMError)
	if !ok {
		return errors.Is(e.Err, target)
	}
	return e.Code == t.Code
}

// newLSMError creates a new LSMError with code, message, and wrapped error.
func newLSMError(code, message string, cause error) *LSMError {
	return &LSMError{
		Code:    code,
		Message: message,
		Err:     cause,
	}
}
