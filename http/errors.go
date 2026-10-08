package http

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"slices"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// ErrorCode is a transport classification with a fixed public message and HTTP
// status. Built-in codes implement error, so a handler may return NotFound or
// wrap it with an ordinary Go error. Custom codes require DefineError; returning
// an undeclared code alone exposes InternalError.
type ErrorCode string

const (
	BadRequest           ErrorCode = "bad_request"
	Unauthenticated      ErrorCode = "unauthenticated"
	Forbidden            ErrorCode = "forbidden"
	MFARequired          ErrorCode = "mfa_required"
	NotFound             ErrorCode = "not_found"
	NotAcceptable        ErrorCode = "not_acceptable"
	MethodNotAllowed     ErrorCode = "method_not_allowed"
	Conflict             ErrorCode = "conflict"
	PreconditionFailed   ErrorCode = "precondition_failed"
	RangeNotSatisfiable  ErrorCode = "range_not_satisfiable"
	ValidationFailed     ErrorCode = "validation_failed"
	PayloadTooLarge      ErrorCode = "payload_too_large"
	UnsupportedMediaType ErrorCode = "unsupported_media_type"
	RequestTimeout       ErrorCode = "request_timeout"
	RateLimited          ErrorCode = "rate_limited"
	InternalError        ErrorCode = "internal_error"
	Unavailable          ErrorCode = "unavailable"
)

// ErrorDefinition is the shared runtime/client-contract description of a code.
// Returned definitions are value snapshots; editing one cannot change behavior.
type ErrorDefinition struct {
	Code    ErrorCode `json:"error_code"`
	Status  int       `json:"status"`
	Message string    `json:"message"`
}

var errorDefinitions = [...]ErrorDefinition{
	{BadRequest, stdhttp.StatusBadRequest, "Invalid request"},
	{Unauthenticated, stdhttp.StatusUnauthorized, "Authentication required"},
	{Forbidden, stdhttp.StatusForbidden, "Access denied"},
	{MFARequired, stdhttp.StatusForbidden, "Additional authentication required"},
	{NotFound, stdhttp.StatusNotFound, "Resource not found"},
	{NotAcceptable, stdhttp.StatusNotAcceptable, "No acceptable response encoding"},
	{MethodNotAllowed, stdhttp.StatusMethodNotAllowed, "Method not allowed"},
	{Conflict, stdhttp.StatusConflict, "Request conflicts with current state"},
	{PreconditionFailed, stdhttp.StatusPreconditionFailed, "Request precondition failed"},
	{RangeNotSatisfiable, stdhttp.StatusRequestedRangeNotSatisfiable, "Requested range is not satisfiable"},
	{ValidationFailed, stdhttp.StatusUnprocessableEntity, "Validation failed"},
	{PayloadTooLarge, stdhttp.StatusRequestEntityTooLarge, "Payload too large"},
	{UnsupportedMediaType, stdhttp.StatusUnsupportedMediaType, "Unsupported media type"},
	{RequestTimeout, stdhttp.StatusRequestTimeout, "Request timed out"},
	{RateLimited, stdhttp.StatusTooManyRequests, "Rate limit exceeded"},
	{InternalError, stdhttp.StatusInternalServerError, "Internal server error"},
	{Unavailable, stdhttp.StatusServiceUnavailable, "Service unavailable"},
}

// ErrorDefinitions returns the built-in catalog used by response encoding.
// Router.ErrorDefinitions adds declared application errors. Contract exporters
// must consume these catalogs instead of copying status maps.
func ErrorDefinitions() []ErrorDefinition { return slices.Clone(errorDefinitions[:]) }

func (c ErrorCode) definition() (ErrorDefinition, bool) {
	for _, definition := range errorDefinitions {
		if definition.Code == c {
			return definition, true
		}
	}
	return ErrorDefinition{}, false
}

func (c ErrorCode) Error() string {
	if definition, ok := c.definition(); ok {
		return string(definition.Code)
	}
	return string(InternalError)
}

// WithCause retains an internal cause for errors.Is/errors.As without including
// its text in the public response or this error's ordinary formatting.
func (c ErrorCode) WithCause(cause error) error { return &responseError{code: c, cause: cause} }

type responseError struct {
	code   ErrorCode
	cause  error
	issues []contract.Issue
}

type classifiedError interface {
	error
	httpErrorCode() ErrorCode
}

func (c ErrorCode) httpErrorCode() ErrorCode              { return c }
func (e responseError) httpErrorCode() ErrorCode          { return e.code }
func (e responseError) Error() string                     { return e.code.Error() }
func (e responseError) httpErrorIssues() []contract.Issue { return slices.Clone(e.issues) }
func (e responseError) Unwrap() error                     { return e.cause }
func (e responseError) Is(target error) bool              { return target == e.code }

// FoundryDiagnostic contributes the public transport code to redacted diagnostics.
func (c ErrorCode) FoundryDiagnostic(errordiag.Seal) []fault.Attribute {
	return []fault.Attribute{{Key: "http_code", Value: string(c)}}
}
func (e responseError) FoundryDiagnostic(errordiag.Seal) []fault.Attribute {
	return e.code.FoundryDiagnostic(errordiag.Seal{})
}

// ErrorResponse is Foundry's public HTTP failure DTO. Models and internal causes
// are never serialized into this envelope.
type ErrorResponse struct {
	Status          int                   `json:"status"`
	Code            ErrorCode             `json:"error_code"`
	Message         string                `json:"message"`
	RequestID       attribution.RequestID `json:"request_id,omitempty"`
	Issues          []contract.Issue      `json:"issues,omitempty"`
	IssuesTruncated bool                  `json:"issues_truncated,omitempty"`
}

func errorResponse(ctx context.Context, err error) (ErrorResponse, error) {
	return localizedErrorResponse(ctx, err, nil)
}

func localizedErrorResponse(ctx context.Context, err error, presenter *errorPresenter) (ErrorResponse, error) {
	code := InternalError
	var custom *ErrorDefinition
	var issues []contract.Issue
	var truncated bool
	var presentationErr error
	// Preserve ordinary Go wrapping/joining while owning arbitrary As/Unwrap
	// methods. Classification never receives a writer, so callback failure
	// cannot leave a partially committed error response. Framework hot paths
	// use callback.Invoke (panic containment, no goroutine); Isolated is kept
	// for application handlers and hooks, where Goexit must also be caught.
	classificationErr := callback.Invoke("HTTP error classification", func() error {
		classified, found, complete := errorgraph.As[classifiedError](err)
		if !complete {
			return fault.New(fault.Invalid, "HTTP error classification exceeded traversal bounds")
		}
		if found {
			code = classified.httpErrorCode()
			if declared, ok := classified.(interface {
				httpErrorDefinition() (ErrorDefinition, bool)
			}); ok {
				if definition, valid := declared.httpErrorDefinition(); valid && definition.Code == code {
					custom = &definition
				}
			}
			if detailed, ok := classified.(interface{ httpErrorIssues() []contract.Issue }); ok {
				issues = detailed.httpErrorIssues()
			}
		} else {
			rejected, found, complete := errorgraph.As[*validation.Errors](err)
			if !complete {
				return fault.New(fault.Invalid, "HTTP validation error classification exceeded traversal bounds")
			}
			if found {
				code = ValidationFailed
				issues, truncated = rejected.Issues(), rejected.Truncated()
				if presenter != nil {
					localized, failure := rejected.Localize(ctx, presenter.catalog, presenter.locale)
					if failure != nil {
						presentationErr = failure
					} else {
						issues = localized.Issues()
					}
				}
			} else if mapped, found := authenticationCode(err); found {
				code = mapped
			}
		}
		return nil
	})
	if classificationErr != nil {
		code = InternalError
		custom = nil
		issues = nil
		truncated = false
	}
	definition, valid := code.definition()
	if custom != nil {
		if allowsError(ctx, *custom) {
			definition, valid = *custom, true
		} else {
			valid = false
			classificationErr = fault.New(fault.Invalid, "endpoint returned an undeclared HTTP error")
		}
	}
	if !valid {
		definition, _ = InternalError.definition()
		issues = nil
		truncated = false
	}
	payload := ErrorResponse{Status: definition.Status, Code: definition.Code, Message: definition.Message, RequestID: RequestID(ctx), Issues: issues, IssuesTruncated: truncated}
	if presenter != nil {
		presenter.present(ctx, &payload)
	}
	if classificationErr == nil {
		classificationErr = presentationErr
	}
	return payload, classificationErr
}

// WriteError sends the shared JSON error envelope to a fresh response. Call it
// before writing status/body; a response already sent cannot be replaced. Nil
// errors are rejected. Internal causes are retained by their originating error,
// never exposed in JSON. Raw handlers may use this explicit transport adapter.
// Classification runs custom error methods on the caller's goroutine; a panic
// becomes a safe internal response before any headers or body are written;
// a panic during retry lookup retains the selected response and omits its hint.
// runtime.Goexit ends the calling goroutine as any Go call does. Error
// methods must terminate and be safe for concurrent classification calls. Each
// search is bounded to 256 nodes and 64 nested levels; exhausted classification
// becomes InternalError, while an exhausted retry lookup omits Retry-After.
func WriteError(w stdhttp.ResponseWriter, r *stdhttp.Request, err error) error {
	if w == nil || r == nil || err == nil {
		return fault.New(fault.Invalid, "HTTP error response requires a writer, request and error")
	}
	// The error is the matched route's final response: response wrappers
	// deliver it even after the route's deadline, unless the client left.
	completeRoute(r.Context())
	presenter, presenterErr := findErrorPresenter(w)
	if presenterErr != nil {
		logRouteFailure(r, "HTTP message presenter lookup failed", presenterErr)
	}
	payload, classificationErr := localizedErrorResponse(r.Context(), err, presenter)
	if classificationErr != nil {
		logRouteFailure(r, "HTTP error classification failed", classificationErr)
	}
	if payload.Status >= 500 {
		reportServerFailure(r, payload, err)
	}
	body, encodeErr := json.Marshal(payload)
	if encodeErr != nil {
		return fault.Wrap(fault.Internal, "HTTP error response could not be encoded", encodeErr)
	}
	// Clear representation-specific headers a decoder or handler may have set
	// before failing. Preserve correlation, cookies, CORS and other policy headers.
	header := w.Header()
	clearResponseRepresentation(header)
	header.Set("Content-Type", "application/json")
	header.Set("Cache-Control", "no-store")
	if presenter != nil {
		header.Set("Content-Language", string(presenter.locale))
		appendVary(header, "Accept-Language")
	}
	header.Set("X-Content-Type-Options", "nosniff")
	if payload.Code == RateLimited {
		if retryErr := rateLimitedHeaders(header, err); retryErr != nil {
			logRouteFailure(r, "HTTP lockout retry classification failed", retryErr)
		}
	}
	if payload.Code == Unavailable && header.Get("Retry-After") == "" {
		var overloaded bool
		failure := callback.Invoke("HTTP overload retry classification", func() error {
			overloaded = errorgraph.Is(err, fault.Overloaded)
			return nil
		})
		if failure != nil {
			logRouteFailure(r, "HTTP overload retry classification failed", failure)
		} else if overloaded {
			// Capacity waits already queued this request; a short retry is useful.
			header.Set("Retry-After", "1")
		}
	}
	if payload.Code == IdempotencyInProgress.definition.Code || payload.Code == IdempotencyCapacity.definition.Code || payload.Code == IdempotencyUnavailable.definition.Code {
		var retry time.Duration
		failure := callback.Invoke("HTTP idempotency retry classification", func() error {
			rejected, found, complete := errorgraph.As[*idempotencyRetryError](err)
			if !complete {
				return fault.New(fault.Invalid, "HTTP idempotency retry classification exceeded traversal bounds")
			}
			if found && rejected.retry > 0 && rejected.retry <= 5*time.Minute {
				retry = rejected.retry
			}
			return nil
		})
		if failure != nil {
			logRouteFailure(r, "HTTP idempotency retry classification failed", failure)
		} else if retry > 0 {
			header.Set("Retry-After", rateLimitSeconds(retry))
		}
	}
	if payload.RequestID == "" {
		header.Del(RequestIDHeader)
	} else {
		header.Set(RequestIDHeader, string(payload.RequestID))
	}
	if payload.Code == PayloadTooLarge && r.ProtoMajor == 1 {
		// A declared oversized body may not have been sent yet (100-continue).
		// Avoid draining it before delivering the rejection on HTTP/1.x.
		header.Set("Connection", "close")
	}
	w.WriteHeader(payload.Status)
	if r.Method == stdhttp.MethodHead {
		return nil
	}
	_, writeErr := w.Write(append(body, '\n'))
	if writeErr != nil {
		return fault.Wrap(fault.Internal, "HTTP error response could not be written", writeErr)
	}
	return nil
}

func clearResponseRepresentation(header stdhttp.Header) {
	for name := range header {
		canonical := stdhttp.CanonicalHeaderKey(name)
		switch canonical {
		case "Content-Type", "Content-Length", "Content-Encoding", "Accept-Ranges", "Content-Disposition", "Content-Range", "Content-Location", "Content-Language", "Content-Md5", "Content-Digest", "Repr-Digest", "Digest", "Etag", "Last-Modified", "Trailer", "Transfer-Encoding":
			delete(header, name)
		default:
			if strings.HasPrefix(name, stdhttp.TrailerPrefix) {
				delete(header, name)
			}
		}
	}
}
