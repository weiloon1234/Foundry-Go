package http

import (
	stdhttp "net/http"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// DescribePathCodec exposes the same scalar metadata used by path/query/cookie
// declarations for other typed text boundaries. Custom codecs use DescribeURL.
func DescribePathCodec[V any](codec PathCodec[V]) (URLScalarInfo, error) {
	if !validPathCodec(codec) {
		return URLScalarInfo{}, safeerror.NewFault(fault.Invalid, "scalar requires a codec")
	}
	described, ok := codec.(urlScalarDescriber)
	if !ok {
		return URLScalarInfo{}, safeerror.NewFault(fault.Invalid, "scalar codec requires explicit metadata")
	}
	return described.urlScalar()
}

// RequestOrigin returns the actual scheme/authority after trusted proxy handling.
// It does not apply a canonical URL override or prove host admission. Use
// PublicURLs middleware to constrain public hosts where required.
func RequestOrigin(r *stdhttp.Request) (Origin, error) {
	if r == nil || r.URL == nil {
		return "", safeerror.NewFault(fault.Invalid, "origin requires a request")
	}
	return requestOrigin(r)
}
