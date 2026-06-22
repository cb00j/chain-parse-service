package errors

import (
	"fmt"
	"time"
)

// ErrorCode represents a specific error code in the unified system.
type ErrorCode string

// Storage error codes (the only codes actively used)
const (
	StoreConnectionError ErrorCode = "E301"
	StoreQueryError      ErrorCode = "E302"
	StoreInsertError     ErrorCode = "E303"
	StoreTransactionErr  ErrorCode = "E304"
)

// ParseError is the unified error type for parser operations.
type ParseError struct {
	Code      ErrorCode
	Message   string
	Wrapped   error
	Timestamp time.Time
	Module    string
	Operation string
}

// New creates a new ParseError.
func New(code ErrorCode, message string) *ParseError {
	return &ParseError{
		Code:      code,
		Message:   message,
		Timestamp: time.Now(),
	}
}

// Wrap wraps an existing error with additional context.
func Wrap(code ErrorCode, message string, err error) *ParseError {
	pe := New(code, message)
	pe.Wrapped = err
	return pe
}

// WithModule sets the module that generated the error.
func (e *ParseError) WithModule(module string) *ParseError {
	e.Module = module
	return e
}

// WithOperation sets the operation that failed.
func (e *ParseError) WithOperation(operation string) *ParseError {
	e.Operation = operation
	return e
}

// Error implements the error interface.
func (e *ParseError) Error() string {
	if e.Wrapped != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Wrapped)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap returns the wrapped error for errors.Is/As compatibility.
func (e *ParseError) Unwrap() error {
	return e.Wrapped
}

// NewStorageError creates a storage layer error.
func NewStorageError(code ErrorCode, message string, err error) *ParseError {
	return Wrap(code, message, err).WithModule("storage")
}

// NewDexError creates a DEX extractor error.
func NewDexError(code ErrorCode, message string, err error) *ParseError {
	return Wrap(code, message, err).WithModule("dex")
}
