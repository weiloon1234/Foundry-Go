package http

import (
	"context"
	"errors"
	"io"
	"mime"
	stdhttp "net/http"
	"strings"
	"sync/atomic"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// maxRawMediaTypes bounds the media types one raw request body declares.
const maxRawMediaTypes = 16

// RawBodyLimits bounds a raw request body. The route's WithBodyLimit (or the
// kernel MaxBodyBytes) still applies; raise both for large uploads. A zero
// value is allowed on endpoints without a raw body.
type RawBodyLimits struct {
	Bytes int64
}

func (l RawBodyLimits) Validate() error {
	if l.Bytes <= 0 || l.Bytes > MaxRouteBodyBytes {
		return fault.New(fault.Invalid, "raw request body limit must be positive and at most MaxRouteBodyBytes")
	}
	return nil
}

// RawBodyInfo describes a raw request body's accepted media types.
type RawBodyInfo struct {
	MediaTypes []MediaType `json:"media_types"`
}

// RawBody is a typed endpoint's streaming request body of a declared media
// type. The handler reads it; nothing is buffered in advance. Reads are bounded
// by EndpointLimits.Raw.Bytes and the route's body limit. Read errors are
// framework errors a handler may return as-is: an oversized body is 413, a
// client that sends too slowly is 408 and a broken transfer is 400. The body
// closes when the handler returns. Preparation, authorization and validation
// receive it too, but must not read it.
type RawBody struct {
	reader *rawBodyReader
	media  MediaType
	length value.Optional[int64]
}

// RawRequestBody declares a raw request body accepting the given media types,
// for example application/octet-stream. The request Content-Type must match one
// of them (parameters such as charset are kept and not compared) with identity
// Content-Encoding; otherwise the request is rejected with 415 before hooks run.
// Metadata, OpenAPI and generated clients describe it as binary content.
func RawRequestBody(media MediaType, additional ...MediaType) Body[RawBody] {
	declared := append([]MediaType{media}, additional...)
	return Body[RawBody]{kind: payloadRaw, raw: &rawBodyDescriptor[RawBody]{media: declared, open: func(w stdhttp.ResponseWriter, r *stdhttp.Request, limits RawBodyLimits) (RawBody, func() error, error) {
		return openRawBody(w, r, declared, limits)
	}}}
}

type rawBodyDescriptor[B any] struct {
	media []MediaType
	open  func(stdhttp.ResponseWriter, *stdhttp.Request, RawBodyLimits) (B, func() error, error)
}

// Read reads the request body. It fails once the handler has returned.
func (b RawBody) Read(data []byte) (int, error) {
	if b.reader == nil {
		return 0, fault.New(fault.Invalid, "raw request body is not open")
	}
	return b.reader.Read(data)
}

// MediaType returns the request's Content-Type, which matched a declared media type.
func (b RawBody) MediaType() MediaType { return b.media }

// Length returns the declared Content-Length; unset means unknown (chunked).
func (b RawBody) Length() value.Optional[int64] { return b.length }

// MarshalJSON rejects implicit serialization; a raw body is not a JSON value.
func (RawBody) MarshalJSON() ([]byte, error) {
	return nil, fault.New(fault.Invalid, "raw request body is not a JSON value")
}

type rawBodyReader struct {
	ctx    context.Context
	body   io.Reader
	closed atomic.Bool
}

func (r *rawBodyReader) Read(data []byte) (int, error) {
	if r.closed.Load() {
		return 0, fault.New(fault.Closed, "raw request body was read after its handler returned")
	}
	n, err := r.body.Read(data)
	if err == nil || err == io.EOF {
		return n, err
	}
	return n, rawBodyError(r.ctx, err)
}

func (r *rawBodyReader) close() error { r.closed.Store(true); return nil }

// rawBodyError classifies a native read failure for the handler's caller.
func rawBodyError(ctx context.Context, err error) error {
	var tooLarge *stdhttp.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		return PayloadTooLarge.WithCause(err)
	case ctx.Err() != nil:
		return RequestTimeout.WithCause(err)
	default:
		return BadRequest.WithCause(err)
	}
}

func openRawBody(w stdhttp.ResponseWriter, r *stdhttp.Request, declared []MediaType, limits RawBodyLimits) (RawBody, func() error, error) {
	if err := r.Context().Err(); err != nil {
		return RawBody{}, nil, RequestTimeout.WithCause(err)
	}
	if r.ContentLength > limits.Bytes {
		rejectUnreadBody(w, r)
		return RawBody{}, nil, PayloadTooLarge
	}
	media, ok := rawRequestMedia(r.Header, declared)
	if !ok {
		rejectUnreadBody(w, r)
		return RawBody{}, nil, UnsupportedMediaType
	}
	var length value.Optional[int64]
	if r.ContentLength >= 0 {
		length = value.Set(r.ContentLength)
	}
	var source io.Reader = stdhttp.NoBody
	if r.Body != nil && r.Body != stdhttp.NoBody {
		source = stdhttp.MaxBytesReader(w, r.Body, limits.Bytes)
	}
	reader := &rawBodyReader{ctx: r.Context(), body: source}
	return RawBody{reader: reader, media: media, length: length}, reader.close, nil
}

// rawRequestMedia accepts exactly one Content-Type whose media type equals a
// declared one, with identity Content-Encoding.
func rawRequestMedia(header stdhttp.Header, declared []MediaType) (MediaType, bool) {
	values := header.Values("Content-Type")
	if len(values) != 1 || !identityRequestEncoding(header) || HeaderValue(values[0]).Validate() != nil {
		return "", false
	}
	media, _, err := mime.ParseMediaType(values[0])
	if err != nil {
		return "", false
	}
	for _, candidate := range declared {
		if expected, _, _ := mime.ParseMediaType(string(candidate)); strings.EqualFold(expected, media) {
			return MediaType(values[0]), true
		}
	}
	return "", false
}

func validateRawMedia(media []MediaType) error {
	if len(media) == 0 || len(media) > maxRawMediaTypes {
		return fault.New(fault.Invalid, "raw request body requires one to sixteen media types")
	}
	seen := make(map[string]bool, len(media))
	for _, candidate := range media {
		if err := candidate.Validate(); err != nil {
			return err
		}
		base, _, _ := mime.ParseMediaType(string(candidate))
		if seen[base] {
			return fault.New(fault.Duplicate, "raw request body media types repeat")
		}
		seen[base] = true
	}
	return nil
}
