package http

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/httpheader"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/value"
)

// HeaderParam declares one required request header on the endpoint's Q value.
// It reuses query scalar codecs without URL escaping. Reserved names are invalid;
// configured proxy and third-party middleware inputs must not be declared either.
// Example: HeaderParam("X-App-Version", StringQuery[string](), func(q *Params) *string { return &q.Version })
func HeaderParam[Q, V any](name HeaderName, codec QueryCodec[V], field func(*Q) *V) QueryParameter[Q] {
	return requestHeaderParameter(QueryParam(string(name), codec, field))
}

// OptionalHeaderParam preserves omission separately from a supplied empty value.
// Header names are case-insensitive; metadata uses HeaderName's canonical casing.
// Example: OptionalHeaderParam("X-Device-ID", StringQuery[string](), func(q *Params) *value.Optional[string] { return &q.Device })
func OptionalHeaderParam[Q, V any](name HeaderName, codec QueryCodec[V], field func(*Q) *value.Optional[V]) QueryParameter[Q] {
	return requestHeaderParameter(OptionalQueryParam(string(name), codec, field))
}

func requestHeaderParameter[Q any](parameter QueryParameter[Q]) QueryParameter[Q] {
	parameter.source = headerParameterSource
	if err := httpheader.ValidateRequestName(parameter.info.Name); err != nil {
		parameter.err = err
		return parameter
	}
	parameter.info.Name, parameter.err = httpheader.CanonicalName(parameter.info.Name)
	return parameter
}

// RequestHeaderParameters returns an owned, canonical-name-sorted snapshot of
// the header bindings, excluding query bindings. Header defaults and repetition
// are unsupported; scalar metadata is shared with the actual typed codec.
// Example: headers, err := RequestHeaderParameters(parameters)
func RequestHeaderParameters[Q any](d Query[Q]) ([]QueryParameterInfo, error) {
	return queryParameterMetadata(d, headerParameterSource)
}

// RequestHeaderLimits bounds declared headers independently of query/body input.
// Bytes counts canonical-name and raw-value bytes per supplied field line;
// FieldBytes bounds the sum of raw value bytes for one declared name.
// Example: limits := DefaultEndpointLimits().Headers
type RequestHeaderLimits struct {
	FieldBytes int
	Bytes      int
	Fields     int
	Values     int
	Issues     int
}

// Validate requires positive, consistent bounds within the header-value ceiling.
func (l RequestHeaderLimits) Validate() error {
	if l.FieldBytes <= 0 || l.Bytes <= 0 || l.Fields <= 0 || l.Values <= 0 || l.Issues <= 0 ||
		l.FieldBytes > MaxHeaderValueBytes || l.FieldBytes > l.Bytes || l.Fields > l.Values {
		return safeerror.NewFault(fault.Invalid, "invalid request header limits")
	}
	return nil
}

func validateRequestHeaderDeclarations[Q any](d Query[Q], limits RequestHeaderLimits) error {
	if len(d.headerIndexes) == 0 {
		return nil
	}
	if err := limits.Validate(); err != nil {
		return err
	}
	if len(d.headerIndexes) > limits.Fields {
		return safeerror.NewFault(fault.Invalid, "request header declarations exceed field limit")
	}
	remaining := limits.Bytes
	for name := range d.headerIndexes {
		if len(name) > remaining {
			return safeerror.NewFault(fault.Invalid, "request header declarations exceed byte limit")
		}
		remaining -= len(name)
	}
	return nil
}
