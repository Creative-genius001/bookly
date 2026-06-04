package errorMap

import (
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"
)

type Code int

const (
	CodeUnknown       Code = iota // unexpected / unclassified
	CodeNotFound                  // resource does not exist
	CodeAlreadyExists             // resource conflicts with an existing one
	CodeInvalidInput              // caller supplied bad data
	CodeUnauthorized              // authentication required or failed
	CodeForbidden                 // authenticated but not permitted
	CodeTimeout                   // deadline exceeded
	CodeUnavailable               // service / dependency down
	CodeInternal                  // bug or unhandled condition inside this service
)

func (c Code) String() int {
	switch c {
	case CodeNotFound:
		return http.StatusNotFound
	case CodeAlreadyExists:
		return http.StatusConflict
	case CodeInvalidInput:
		return http.StatusBadRequest
	case CodeUnauthorized:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeTimeout:
		return http.StatusGatewayTimeout
	case CodeUnavailable:
		return http.StatusServiceUnavailable
	case CodeInternal:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

func (c Code) HTTPStatus() int {
	switch c {
	case CodeNotFound:
		return http.StatusNotFound
	case CodeAlreadyExists:
		return http.StatusConflict
	case CodeInvalidInput:
		return http.StatusBadRequest
	case CodeUnauthorized:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeTimeout:
		return http.StatusGatewayTimeout
	case CodeUnavailable:
		return http.StatusServiceUnavailable
	case CodeInternal:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

type AppError struct {
	Code    Code              // canonical classification
	Message string            // human-readable, safe to expose to callers
	Detail  string            // optional extra detail (internal logs only)
	Fields  map[string]string // field-level validation errors
	Op      string            // operation where the error originated
	Err     error             // wrapped upstream cause
	Stack   string            // captured call stack (populated by New/Wrap)
	Time    time.Time         // when the error was created
}

func (e *AppError) Error() string {
	var sb strings.Builder
	if e.Op != "" {
		sb.WriteString(e.Op)
		sb.WriteString(": ")
	}
	sb.WriteString(fmt.Sprintf("[%s] %s", e.Code, e.Message))
	if e.Detail != "" {
		sb.WriteString(" — ")
		sb.WriteString(e.Detail)
	}
	if e.Err != nil {
		sb.WriteString(": ")
		sb.WriteString(e.Err.Error())
	}
	return sb.String()
}

func (e *AppError) Unwrap() error { return e.Err }

func (e *AppError) Is(target error) bool {
	var t *AppError
	if errors.As(target, &t) {
		return e.Code == t.Code
	}
	return false
}

func New(code Code, op, message string) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
		Op:      op,
		Stack:   captureStack(2),
		Time:    time.Now().UTC(),
	}
}

// Wrap enriches an existing error with classification and operation context.
// If err is nil, Wrap returns nil — safe to use inline with return values.
func Wrap(err error, code Code, op, message string) *AppError {
	if err == nil {
		return nil
	}
	return &AppError{
		Code:    code,
		Message: message,
		Op:      op,
		Err:     err,
		Stack:   captureStack(2),
		Time:    time.Now().UTC(),
	}
}

// Wrapf is like Wrap but accepts a format string for the message.
func Wrapf(err error, code Code, op, format string, args ...any) *AppError {
	return Wrap(err, code, op, fmt.Sprintf(format, args...))
}

// ---------------------------------------------------------------------------
// Fluent setters (builder-style)
// ---------------------------------------------------------------------------

// WithDetail attaches internal detail (not safe for external callers).
func (e *AppError) WithDetail(detail string) *AppError {
	e.Detail = detail
	return e
}

// WithField adds a single field-level validation error.
func (e *AppError) WithField(field, reason string) *AppError {
	if e.Fields == nil {
		e.Fields = make(map[string]string)
	}
	e.Fields[field] = reason
	return e
}

// WithFields adds multiple field-level validation errors.
func (e *AppError) WithFields(fields map[string]string) *AppError {
	if e.Fields == nil {
		e.Fields = make(map[string]string)
	}
	for k, v := range fields {
		e.Fields[k] = v
	}
	return e
}

// ---------------------------------------------------------------------------
// Interrogation helpers
// ---------------------------------------------------------------------------

// CodeOf extracts the Code from any error in the chain.
// Falls back to CodeUnknown if none is found.
func CodeOf(err error) Code {
	var e *AppError
	if errors.As(err, &e) {
		return e.Code
	}
	return CodeUnknown
}

// MessageOf returns the public-facing message, or a generic fallback.
func MessageOf(err error) string {
	var e *AppError
	if errors.As(err, &e) {
		return e.Message
	}
	return "an unexpected error occurred"
}

// Is* convenience predicates — mirror the pattern in the stdlib.

func IsNotFound(err error) bool     { return CodeOf(err) == CodeNotFound }
func IsInvalidInput(err error) bool { return CodeOf(err) == CodeInvalidInput }
func IsUnauthorized(err error) bool { return CodeOf(err) == CodeUnauthorized }
func IsForbidden(err error) bool    { return CodeOf(err) == CodeForbidden }
func IsInternal(err error) bool     { return CodeOf(err) == CodeInternal }
func IsTimeout(err error) bool      { return CodeOf(err) == CodeTimeout }
func IsUnavailable(err error) bool  { return CodeOf(err) == CodeUnavailable }

// ---------------------------------------------------------------------------
// HTTP response helper
// ---------------------------------------------------------------------------

func HTTPStatus(err error) int {
	return CodeOf(err).HTTPStatus()
}

type ErrorResponse struct {
	Code    int               `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func ToHTTPResponse(err error) (int, ErrorResponse) {
	var e *AppError
	if errors.As(err, &e) {
		return e.Code.HTTPStatus(), ErrorResponse{
			Code:    e.Code.String(),
			Message: e.Message,
			Fields:  e.Fields,
		}
	}
	return http.StatusInternalServerError, ErrorResponse{
		Code:    CodeUnknown.String(),
		Message: "an unexpected error occurred",
	}
}

func captureStack(skip int) string {
	pcs := make([]uintptr, 16)
	n := runtime.Callers(skip+2, pcs)
	frames := runtime.CallersFrames(pcs[:n])

	var sb strings.Builder
	for {
		f, more := frames.Next()
		if !strings.Contains(f.File, "runtime/") {
			fmt.Fprintf(&sb, "%s\n\t%s:%d\n", f.Function, f.File, f.Line)
		}
		if !more {
			break
		}
	}
	return sb.String()
}
