package http

import (
	"context"
	stdhttp "net/http"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
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
	kind      payloadKind
	json      contract.JSON[B]
	multipart Multipart[B]
	form      Query[B]
	raw       *rawBodyDescriptor[B]
	// refreshCookie reads a refresh credential from its cookie instead of a body.
	refreshCookie *RefreshCookie
	fromCookie    func(*stdhttp.Request) (B, error)
}

func JSONBody[B any](descriptor contract.JSON[B]) Body[B] {
	return Body[B]{kind: payloadJSON, json: descriptor}
}

// MultipartBody declares a generated or handwritten typed multipart form.
func MultipartBody[B any](descriptor Multipart[B]) Body[B] {
	return Body[B]{kind: payloadMultipart, multipart: descriptor}
}

// FormBody uses the existing typed URL bindings as an independent URL-encoded
// request body. Query parameters never satisfy these body fields. Reuse generated
// form/query descriptors and MergeQueries/EmbedQuery for explicit composition.
func FormBody[B any](fields Query[B]) Body[B] { return Body[B]{kind: payloadForm, form: fields} }

func EmptyBody() Body[NoBody] { return Body[NoBody]{kind: payloadEmpty} }

func (b Body[B]) Validate() error {
	switch b.kind {
	case payloadJSON:
		return b.json.Validate()
	case payloadMultipart:
		return b.multipart.Validate()
	case payloadForm:
		return b.form.Validate()
	case payloadRaw:
		if b.raw == nil || b.raw.open == nil {
			return fault.New(fault.Invalid, "raw request body is not defined")
		}
		return validateRawMedia(b.raw.media)
	case payloadEmpty:
		return nil
	case payloadRefreshCookie:
		if b.refreshCookie == nil || b.fromCookie == nil {
			return fault.New(fault.Invalid, "refresh cookie body is not defined")
		}
		return b.refreshCookie.Validate()
	default:
		return fault.New(fault.Invalid, "request body contract is not defined")
	}
}

// responseJSON allows framework-owned credential delivery to describe its wire
// DTO while retaining a non-serializable, model-owned handler result.
type responseJSON[R any] interface {
	Validate() error
	Description() (contract.Schema, error)
	Encode(context.Context, R, contract.JSONLimits) ([]byte, error)
}

// Response is a declared success payload and status. It retains the response
// DTO type at handler registration; its zero value is invalid.
type Response[R any] struct {
	kind        payloadKind
	status      int
	json        responseJSON[R]
	credentials bool
	file        *fileResponse
	prepareFile func(context.Context, R, FileResponseLimits) (*preparedFile, error)
	// statuses lists every declared success status, primary first, when a
	// JSON response declares alternatives; selectStatus reads the handler's choice.
	statuses     []int
	selectStatus func(R) int
	// redirectTarget reads a redirect result's validated location.
	redirectTarget func(R) (string, error)
	// events is a typed server-sent event stream contract.
	events eventResponse[R]
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
	return Response[R]{kind: payloadJSON, status: status, json: descriptor}
}

// EmptyResponse supports 204 No Content and 205 Reset Content.
func EmptyResponse(status int) Response[NoContent] {
	return Response[NoContent]{kind: payloadEmpty, status: status}
}

func (r Response[R]) Validate() error {
	if r.refreshCookieErr != nil {
		return r.refreshCookieErr
	}
	if r.refreshCookie != nil {
		if err := r.refreshCookie.Validate(); err != nil {
			return err
		}
	}
	switch r.kind {
	case payloadDownload, payloadStream:
		if r.status != 200 || r.file == nil || r.prepareFile == nil {
			return fault.New(fault.Invalid, "file response is not defined")
		}
		return r.file.Validate()
	case payloadJSON:
		if r.json == nil || !bodySuccessStatus(r.status) {
			return fault.New(fault.Invalid, "JSON response requires a body-bearing success status")
		}
		if r.statuses != nil {
			if err := validateResponseStatuses(r.statuses, r.status); err != nil || r.selectStatus == nil {
				return fault.New(fault.Invalid, "JSON response alternatives require two to eight unique body-bearing success statuses")
			}
		}
		return r.json.Validate()
	case payloadRedirect:
		if !redirectStatus(r.status) || r.redirectTarget == nil {
			return fault.New(fault.Invalid, "redirect response requires status 301, 302, 303, 307 or 308")
		}
		return nil
	case payloadEvents:
		if r.status != 200 || r.events == nil {
			return fault.New(fault.Invalid, "event stream response is not defined")
		}
		return r.events.Validate()
	case payloadEmpty:
		if r.status == 204 || r.status == 205 {
			return nil
		}
	}
	return fault.New(fault.Invalid, "response contract is not defined or has an invalid status")
}

func bodySuccessStatus(status int) bool {
	return status >= 200 && status <= 299 && status != 204 && status != 205
}
