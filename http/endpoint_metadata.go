package http

import (
	"encoding/json"
	"slices"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// PayloadInfo describes the JSON, form, multipart or file contract used at runtime.
// EndpointInfo uses nil payload pointers for empty bodies/responses.
// Example is a documented JSON example already encoded by the same contract.
type PayloadInfo struct {
	MediaType string               `json:"media_type,omitempty"`
	Schema    contract.Schema      `json:"schema,omitzero"`
	Form      []QueryParameterInfo `json:"form,omitempty"`
	Multipart *MultipartInfo       `json:"multipart,omitempty"`
	File      *FileResponseInfo    `json:"file,omitempty"`
	Raw       *RawBodyInfo         `json:"raw,omitempty"`
	Example   json.RawMessage      `json:"example,omitempty"`
}

// EndpointInfo is an owned snapshot of typed endpoint declarations. Query
// cardinality and codec-owned scalar metadata share the runtime declarations.
// Undescribed custom codecs retain nil metadata instead of a fabricated schema.
type EndpointInfo struct {
	// TableOperation is the explicitly declared table association.
	TableOperation *contract.TableOperationBinding `json:"table_operation,omitempty"`
	Path           []PathParameterInfo             `json:"path"`
	Route          RouteInfo                       `json:"route"`
	Query          []QueryParameterInfo            `json:"query"`
	Body           *PayloadInfo                    `json:"body,omitempty"`
	Response       *PayloadInfo                    `json:"response,omitempty"`
	Status         int                             `json:"status"`
	// Statuses lists every declared success status, primary first, when the
	// response declares alternatives (JSONResponses).
	Statuses []int `json:"statuses,omitempty"`
	// Redirect marks a RedirectResponse: Status is 3xx with a Location and no body.
	Redirect    bool                    `json:"redirect,omitempty"`
	Limits      EndpointLimits          `json:"limits"`
	Preparation bool                    `json:"preparation,omitempty"`
	Validation  *validation.Description `json:"validation,omitempty"`
	Idempotency *IdempotencyInfo        `json:"idempotency,omitempty"`
	Errors      []ErrorDefinition       `json:"errors,omitempty"`
	// RefreshCookie describes a browser refresh-token cookie the endpoint
	// reads, sets or clears; it never contains a credential value.
	RefreshCookie *RefreshCookieInfo `json:"refresh_cookie,omitempty"`
}

// InspectEndpoint validates before returning owned metadata for inspection.
func InspectEndpoint[P, Q, B, R any](e Endpoint[P, Q, B, R]) (EndpointInfo, error) {
	if err := ValidateEndpoint(e); err != nil {
		return EndpointInfo{}, err
	}
	segments, _ := routeValidate(e.route)
	return endpointSnapshot(e, routeInfo(e.route, segments, false)), nil
}

// snapshot is called only for validated immutable descriptors. Description
// owns schema cloning, including nested enum cases; no second clone algorithm
// or schema inference lives in HTTP.
func endpointSnapshot[P, Q, B, R any](e Endpoint[P, Q, B, R], route RouteInfo) EndpointInfo {
	query, _ := QueryParameters(e.query)
	path, _ := PathParameters(e.route.path)
	info := EndpointInfo{Path: path, Route: route.clone(), Query: query, Status: e.response.status, Statuses: slices.Clone(e.response.statuses), Redirect: e.response.kind == payloadRedirect, Limits: e.limits}
	if e.hasTableOperation {
		binding := e.tableOperation
		info.TableOperation = &binding
	}
	info.Idempotency = e.idempotency.clone()
	info.RefreshCookie = endpointRefreshCookieInfo(e)
	info.Preparation = e.preparation != nil
	info.Errors, _ = endpointErrorDefinitions(e)
	if e.validation != nil {
		description, _ := validation.DescribeRule(*e.validation)
		info.Validation = &description
	}
	if e.body.describe != nil {
		info.Body = e.body.describe()
		if e.body.kind == payloadJSON {
			info.Body.Example = slices.Clone(e.examples.body)
		}
	}
	if e.response.describe != nil {
		info.Response = e.response.describe()
		if e.response.kind == payloadJSON {
			info.Response.Example = slices.Clone(e.examples.response)
		}
	}
	return info
}

// Endpoints returns typed metadata in route-ID order. Raw handlers remain
// visible through Routes and do not acquire fabricated payload schemas.
func (r *Router) Endpoints() []EndpointInfo {
	if r == nil {
		return nil
	}
	result := make([]EndpointInfo, 0, len(r.endpoints))
	for _, route := range r.routes {
		if snapshot := r.endpoints[route.ID]; snapshot != nil {
			result = append(result, snapshot())
		}
	}
	return result
}

// schemaPayloadInfo delegates schema ownership to the selected codec.
func schemaPayloadInfo(media string, describe func() (contract.Schema, error)) func() *PayloadInfo {
	return func() *PayloadInfo {
		schema, _ := describe()
		return &PayloadInfo{MediaType: media, Schema: schema}
	}
}

func filePayloadInfo(descriptor *fileResponse) func() *PayloadInfo {
	return func() *PayloadInfo {
		file := descriptor.description()
		info := &PayloadInfo{File: &file}
		if len(file.MediaTypes) == 1 {
			info.MediaType = string(file.MediaTypes[0])
		}
		return info
	}
}
