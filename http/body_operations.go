package http

import (
	stdhttp "net/http"
	"slices"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

func rawBodyOperations[B any](body Body[B]) Body[B] {
	descriptor := body.raw
	body.validate = func() error {
		if descriptor == nil || descriptor.open == nil {
			return safeerror.NewFault(fault.Invalid, "raw request body is not defined")
		}
		return validateRawMedia(descriptor.media)
	}
	body.read = func(w stdhttp.ResponseWriter, r *stdhttp.Request, limits EndpointLimits) (B, func() error, error) {
		return descriptor.open(w, r, limits.Raw)
	}
	body.describe = func() *PayloadInfo {
		info := &PayloadInfo{Raw: &RawBodyInfo{MediaTypes: slices.Clone(descriptor.media)}}
		if len(descriptor.media) == 1 {
			info.MediaType = string(descriptor.media[0])
		}
		return info
	}
	return body
}
func refreshBodyOperations[B any](body Body[B]) Body[B] {
	cookie, read := body.refreshCookie, body.fromCookie
	body.validate = func() error {
		if cookie == nil || read == nil {
			return safeerror.NewFault(fault.Invalid, "refresh cookie body is not defined")
		}
		return cookie.Validate()
	}
	body.read = func(w stdhttp.ResponseWriter, r *stdhttp.Request, limits EndpointLimits) (B, func() error, error) {
		if _, err := readEndpointBody(w, r, int64(limits.Body.Bytes), true, jsonRequestMedia); err != nil {
			return *new(B), nil, err
		}
		result, err := read(r)
		return result, nil, err
	}
	return body
}
