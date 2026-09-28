package http

import (
	"context"

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
	case payloadEmpty:
		return nil
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
	switch r.kind {
	case payloadDownload, payloadStream:
		if r.status != 200 || r.file == nil || r.prepareFile == nil {
			return fault.New(fault.Invalid, "file response is not defined")
		}
		return r.file.Validate()
	case payloadJSON:
		if r.json == nil || r.status < 200 || r.status > 299 || r.status == 204 || r.status == 205 {
			return fault.New(fault.Invalid, "JSON response requires a body-bearing success status")
		}
		return r.json.Validate()
	case payloadEmpty:
		if r.status == 204 || r.status == 205 {
			return nil
		}
	}
	return fault.New(fault.Invalid, "response contract is not defined or has an invalid status")
}
