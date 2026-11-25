package httpquery

import (
	"github.com/weiloon1234/Foundry-Go/contract"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// doc-example:begin tracking-url-codec
func trackingCodec(TrackingScalar contract.Scalar[TrackingCode]) foundryhttp.QueryCodec[TrackingCode] {
	codec := foundryhttp.DescribeURL(
		foundryhttp.TextQuery[TrackingCode, *TrackingCode](),
		TrackingScalar,
	)
	return codec
}

// doc-example:end tracking-url-codec
