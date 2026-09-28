package http

import (
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// PayloadInfo describes the JSON, form, multipart or file contract used at runtime.
// EndpointInfo uses nil payload pointers for empty bodies/responses.
type PayloadInfo struct {
	MediaType string               `json:"media_type,omitempty"`
	Schema    contract.Schema      `json:"schema,omitzero"`
	Form      []QueryParameterInfo `json:"form,omitempty"`
	Multipart *MultipartInfo       `json:"multipart,omitempty"`
	File      *FileResponseInfo    `json:"file,omitempty"`
}

// EndpointInfo is an owned snapshot of typed endpoint declarations. Query
// cardinality and codec-owned scalar metadata share the runtime declarations.
// Undescribed custom codecs retain nil metadata instead of a fabricated schema.
type EndpointInfo struct {
	Path        []PathParameterInfo     `json:"path"`
	Route       RouteInfo               `json:"route"`
	Query       []QueryParameterInfo    `json:"query"`
	Body        *PayloadInfo            `json:"body,omitempty"`
	Response    *PayloadInfo            `json:"response,omitempty"`
	Status      int                     `json:"status"`
	Limits      EndpointLimits          `json:"limits"`
	Preparation bool                    `json:"preparation,omitempty"`
	Validation  *validation.Description `json:"validation,omitempty"`
	Idempotency *IdempotencyInfo        `json:"idempotency,omitempty"`
	Errors      []ErrorDefinition       `json:"errors,omitempty"`
}

// Description validates before returning owned metadata for inspection.
func (e Endpoint[P, Q, B, R]) Description() (EndpointInfo, error) {
	if err := e.Validate(); err != nil {
		return EndpointInfo{}, err
	}
	segments, _ := e.route.validate()
	return e.snapshot(e.route.info(segments, false)), nil
}

// snapshot is called only for validated immutable descriptors. Description
// owns schema cloning, including nested enum cases; no second clone algorithm
// or schema inference lives in HTTP.
func (e Endpoint[P, Q, B, R]) snapshot(route RouteInfo) EndpointInfo {
	query, _ := e.query.Parameters()
	path, _ := e.route.path.Parameters()
	info := EndpointInfo{Path: path, Route: route.clone(), Query: query, Status: e.response.status, Limits: e.limits}
	info.Idempotency = e.idempotency.clone()
	info.Preparation = e.preparation != nil
	info.Errors, _ = e.errorDefinitions()
	if e.validation != nil {
		description, _ := e.validation.Description()
		info.Validation = &description
	}
	if e.body.kind == payloadJSON {
		schema, _ := e.body.json.Description()
		info.Body = &PayloadInfo{MediaType: "application/json", Schema: schema}
	}
	if e.body.kind == payloadForm {
		fields, _ := e.body.form.Parameters()
		info.Body = &PayloadInfo{MediaType: "application/x-www-form-urlencoded", Form: fields}
	}
	if e.body.kind == payloadMultipart {
		form, _ := e.body.multipart.Description()
		info.Body = &PayloadInfo{MediaType: "multipart/form-data", Multipart: &form}
	}
	if e.response.kind == payloadJSON {
		schema, _ := e.response.json.Description()
		info.Response = &PayloadInfo{MediaType: "application/json", Schema: schema}
	}
	if e.response.kind == payloadDownload || e.response.kind == payloadStream {
		file := e.response.file.description()
		info.Response = &PayloadInfo{File: &file}
		if len(file.MediaTypes) == 1 {
			info.Response.MediaType = string(file.MediaTypes[0])
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
