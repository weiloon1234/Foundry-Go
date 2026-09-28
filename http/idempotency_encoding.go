package http

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"slices"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/idempotency"
)

type replayJSON[R any] interface {
	responseJSON[R]
	Decode(context.Context, []byte, contract.JSONLimits) (R, error)
}
type fingerprintRequest struct {
	Path  string          `json:"path"`
	Query string          `json:"query"`
	Body  json.RawMessage `json:"body"`
}

// Bytes are base64 in storage so whitespace/escapes are not rewritten by an
// enclosing JSON marshal. The exact validated response bytes are sent on replay.
type storedResponse struct {
	Status  int              `json:"status"`
	Body    []byte           `json:"body"`
	Headers []ResponseHeader `json:"headers"`
}

var replayHeaderNames = []HeaderName{"Location", "Content-Language", "Etag"}

func replayHeaders(input []ResponseHeader) ([]ResponseHeader, error) {
	if len(input) > len(replayHeaderNames) {
		return nil, fault.New(fault.Invalid, "too many replay response headers")
	}
	result := slices.Clone(input)
	seen := map[HeaderName]bool{}
	for index := range result {
		h := &result[index]
		name, err := h.Name.Canonical()
		if err != nil {
			return nil, err
		}
		h.Name = name
		if !slices.Contains(replayHeaderNames, h.Name) || seen[h.Name] || len(h.Value) > 4096 || h.Validate() != nil {
			return nil, fault.New(fault.Invalid, "invalid or forbidden replay response header")
		}
		seen[h.Name] = true
	}
	return result, nil
}
func wireDigest(v any) string {
	data, _ := json.Marshal(v)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func (e IdempotentEndpoint[P, Q, B, R]) inputEncoding() idempotency.Input[Input[P, Q, B]] {
	info, _ := e.endpoint.Description()
	identity := wireDigest(struct {
		Path        []PathParameterInfo
		Query       []QueryParameterInfo
		Body        *PayloadInfo
		Preparation bool
	}{info.Path, info.Query, info.Body, info.Preparation})
	return idempotency.DefineInput(identity, func(ctx context.Context, in Input[P, Q, B], limit int) ([]byte, error) {
		path, err := e.endpoint.route.URL(in.Path)
		if err != nil {
			return nil, err
		}
		query, err := e.endpoint.query.Encode(ctx, in.Query, e.endpoint.limits.Query)
		if err != nil {
			return nil, err
		}
		body := []byte("null")
		switch e.endpoint.body.kind {
		case payloadJSON:
			limits := e.endpoint.limits.Body
			limits.Bytes = min(limits.Bytes, limit)
			body, err = e.endpoint.body.json.Encode(ctx, in.Body, limits)
		case payloadForm:
			var form string
			form, err = e.endpoint.body.form.Encode(ctx, in.Body, e.endpoint.limits.Form)
			if err == nil {
				body, err = json.Marshal(form)
			}
		}
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(fingerprintRequest{path, query, body})
		if err == nil && len(data) > limit {
			return nil, PayloadTooLarge
		}
		return data, err
	})
}
func (e IdempotentEndpoint[P, Q, B, R]) outputEncoding() idempotency.Encoding[R] {
	info, _ := e.endpoint.Description()
	identity := wireDigest(struct {
		Format   string
		Response *PayloadInfo
		Status   int
		Headers  bool
	}{"foundry.http.response.v1", info.Response, info.Status, e.headers != nil})
	return idempotency.DefineEncoding(identity, func(ctx context.Context, result R, limit int) ([]byte, error) {
		limits := e.endpoint.limits
		limits.Response.Bytes = min(limits.Response.Bytes, limit)
		response, err := e.endpoint.response.prepare(ctx, result, limits)
		if err != nil {
			return nil, err
		}
		var headers []ResponseHeader
		if e.headers != nil {
			headers, err = e.headers(ctx, result)
			if err != nil {
				return nil, err
			}
			headers, err = replayHeaders(headers)
			if err != nil {
				return nil, err
			}
		}
		return json.Marshal(storedResponse{Status: e.endpoint.response.status, Body: response.data, Headers: headers})
	}, func(ctx context.Context, data []byte, limit int) (R, error) {
		stored, err := decodeStoredResponse(data, e.endpoint.response.status, e.headers != nil)
		if err != nil {
			return *new(R), err
		}
		if e.endpoint.response.kind == payloadEmpty {
			if len(stored.Body) != 0 {
				return *new(R), fault.New(fault.Invalid, "empty replay contains a body")
			}
			return *new(R), nil
		}
		limits := e.endpoint.limits.Response
		limits.Bytes = min(limits.Bytes, limit)
		return e.endpoint.response.json.(replayJSON[R]).Decode(ctx, stored.Body, limits)
	})
}
func decodeStoredResponse(data []byte, status int, allowHeaders bool) (storedResponse, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var stored storedResponse
	if err := decoder.Decode(&stored); err != nil {
		return storedResponse{}, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return storedResponse{}, fault.New(fault.Invalid, "invalid stored response boundary")
	}
	if stored.Status != status || !allowHeaders && len(stored.Headers) != 0 {
		return storedResponse{}, fault.New(fault.Invalid, "stored response policy changed")
	}
	var err error
	stored.Headers, err = replayHeaders(stored.Headers)
	return stored, err
}
