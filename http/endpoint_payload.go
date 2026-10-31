package http

import (
	"context"
	stdhttp "net/http"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

type payloadKind uint8

const (
	payloadUnset payloadKind = iota
	payloadEmpty
	payloadJSON
	payloadMultipart
	payloadDownload
	payloadStream
	payloadForm
	payloadRedirect
	payloadRaw
	payloadEvents
	payloadRefreshCookie
)

// NoBody marks an endpoint that accepts no request body.
type NoBody struct{}

// NoContent is the concrete return value of an empty response handler.
type NoContent struct{}

// Body is a typed request payload contract. Its zero value is invalid.
type Body[B any] struct {
	validate   func() error
	read       func(stdhttp.ResponseWriter, *stdhttp.Request, EndpointLimits) (B, func() error, error)
	describe   func() *PayloadInfo
	kind       payloadKind
	encodeJSON func(context.Context, B, contract.JSONLimits) ([]byte, error)
	decodeJSON func(context.Context, []byte, contract.JSONLimits) (B, error)
	form       Query[B]
	raw        *rawBodyDescriptor[B]
	// refreshCookie reads a refresh credential from its cookie instead of a body.
	refreshCookie *RefreshCookie
	fromCookie    func(*stdhttp.Request) (B, error)
	// refreshLogout reads the cookie optionally, for a cookie-authenticated logout.
	refreshLogout bool
}

func JSONBody[B any](descriptor contract.JSON[B]) Body[B] {
	b := Body[B]{kind: payloadJSON, encodeJSON: descriptor.Encode, decodeJSON: descriptor.Decode, validate: descriptor.Validate, describe: func() *PayloadInfo {
		schema, _ := descriptor.Description()
		return &PayloadInfo{MediaType: "application/json", Schema: schema}
	}}
	source := b
	b.read = func(w stdhttp.ResponseWriter, r *stdhttp.Request, limits EndpointLimits) (B, func() error, error) {
		v, err := bodyRead(source, w, r, limits.Body)
		return v, nil, err
	}
	return b
}

// MultipartBody declares a generated or handwritten typed multipart form.
func MultipartBody[B any](descriptor Multipart[B]) Body[B] {
	return Body[B]{kind: payloadMultipart, validate: func() error { return ValidateMultipart(descriptor) }, read: func(w stdhttp.ResponseWriter, r *stdhttp.Request, limits EndpointLimits) (B, func() error, error) {
		return multipartRead(descriptor, w, r, limits)
	}, describe: func() *PayloadInfo {
		form, _ := InspectMultipart(descriptor)
		return &PayloadInfo{MediaType: "multipart/form-data", Multipart: &form}
	}}
}

// FormBody uses the existing typed URL bindings as an independent URL-encoded
// request body. Query parameters never satisfy these body fields. Reuse generated
// form/query descriptors and MergeQueries/EmbedQuery for explicit composition.
func FormBody[B any](fields Query[B]) Body[B] {
	b := Body[B]{kind: payloadForm, form: fields, validate: func() error { return ValidateQueryDescriptor(fields) }, describe: func() *PayloadInfo {
		form, _ := QueryParameters(fields)
		return &PayloadInfo{MediaType: "application/x-www-form-urlencoded", Form: form}
	}}
	source := b
	b.read = func(w stdhttp.ResponseWriter, r *stdhttp.Request, limits EndpointLimits) (B, func() error, error) {
		v, err := bodyReadForm(source, w, r, limits.Form)
		return v, nil, err
	}
	return b
}

func EmptyBody() Body[NoBody] {
	b := Body[NoBody]{kind: payloadEmpty, validate: func() error { return nil }}
	b.read = func(w stdhttp.ResponseWriter, r *stdhttp.Request, limits EndpointLimits) (NoBody, func() error, error) {
		_, err := readEndpointBody(w, r, int64(limits.Body.Bytes), true, jsonRequestMedia)
		return NoBody{}, nil, err
	}
	return b
}

func ValidateBodyDescriptor[B any](body Body[B]) error {
	if body.validate == nil || body.read == nil {
		return safeerror.NewFault(fault.Invalid, "request body contract is not defined")
	}
	return body.validate()
}

// Response is a declared success payload and status. It retains the response
// DTO type at handler registration; its zero value is invalid.
type Response[R any] struct {
	schema       func() (contract.Schema, error)
	describe     func() *PayloadInfo
	prepare      func(Response[R], context.Context, R, EndpointLimits) (preparedResponse, error)
	kind         payloadKind
	status       int
	validateJSON func() error
	encodeJSON   func(context.Context, R, contract.JSONLimits) ([]byte, error)
	decodeJSON   func(context.Context, []byte, contract.JSONLimits) (R, error)
	credentials  bool
	file         *fileResponse
	prepareFile  func(context.Context, R, FileResponseLimits) (*preparedFile, error)
	// statuses lists every declared success status, primary first, when a
	// JSON response declares alternatives; selectStatus reads the handler's choice.
	statuses     []int
	selectStatus func(R) int
	// redirectTarget reads a redirect result's validated location.
	redirectTarget func(R) (string, error)
	// events is a typed server-sent event stream contract.
	validateEvents func() error
	prepareEvents  func(R, contract.JSONLimits) (func(stdhttp.ResponseWriter, *stdhttp.Request) error, error)
	// refreshCookie is set by TokenCookieResponse (sets) or ClearRefreshCookie
	// (clears); setRefreshCookie formats the cookie for a prepared result.
	refreshCookie       *RefreshCookie
	refreshCookieSets   bool
	refreshCookieClears bool
	refreshCookieErr    error
	setRefreshCookie    func(context.Context, R) (string, error)
}

// JSONResponse declares a success status with a schema-checked JSON payload.
func JSONResponse[R any](status int, descriptor contract.JSON[R]) Response[R] {
	return Response[R]{prepare: prepareJSONResponse[R], kind: payloadJSON, status: status, validateJSON: descriptor.Validate, schema: descriptor.Description, describe: schemaPayloadInfo("application/json", descriptor.Description), encodeJSON: descriptor.Encode, decodeJSON: descriptor.Decode}
}

// EmptyResponse supports 204 No Content and 205 Reset Content.
func EmptyResponse(status int) Response[NoContent] {
	return Response[NoContent]{prepare: prepareEmptyResponse, kind: payloadEmpty, status: status}
}

func ValidateResponse[R any](r Response[R]) error {
	if r.refreshCookieErr != nil {
		return r.refreshCookieErr
	}
	if r.refreshCookie != nil {
		if err := r.refreshCookie.Validate(); err != nil {
			return err
		}
	}
	if r.prepare == nil {
		return safeerror.NewFault(fault.Invalid, "response contract is not defined or has an invalid status")
	}
	switch r.kind {
	case payloadDownload, payloadStream:
		if r.status != 200 || r.file == nil || r.prepareFile == nil {
			return safeerror.NewFault(fault.Invalid, "file response is not defined")
		}
		return r.file.validate()
	case payloadJSON:
		if r.encodeJSON == nil || r.validateJSON == nil || r.describe == nil || !bodySuccessStatus(r.status) {
			return safeerror.NewFault(fault.Invalid, "JSON response requires a body-bearing success status")
		}
		if r.statuses != nil {
			if err := validateResponseStatuses(r.statuses, r.status); err != nil || r.selectStatus == nil {
				return safeerror.NewFault(fault.Invalid, "JSON response alternatives require two to eight unique body-bearing success statuses")
			}
		}
		return r.validateJSON()
	case payloadRedirect:
		if !redirectStatus(r.status) || r.redirectTarget == nil {
			return safeerror.NewFault(fault.Invalid, "redirect response requires status 301, 302, 303, 307 or 308")
		}
		return nil
	case payloadEvents:
		if r.status != 200 || r.prepareEvents == nil || r.validateEvents == nil || r.describe == nil {
			return safeerror.NewFault(fault.Invalid, "event stream response is not defined")
		}
		return r.validateEvents()
	case payloadEmpty:
		if r.status == 204 || r.status == 205 {
			return nil
		}
	}
	return safeerror.NewFault(fault.Invalid, "response contract is not defined or has an invalid status")
}

func bodySuccessStatus(status int) bool {
	return status >= 200 && status <= 299 && status != 204 && status != 205
}
