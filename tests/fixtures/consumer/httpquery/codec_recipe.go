package httpquery

import (
	"github.com/weiloon1234/Foundry-Go/contract"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func trackingCodec(TrackingScalar contract.Scalar[TrackingCode]) foundryhttp.QueryCodec[TrackingCode] {
	// doc-example:begin tracking-url-codec
	codec := foundryhttp.DescribeURL(
		foundryhttp.TextQuery[TrackingCode, *TrackingCode](),
		TrackingScalar,
	)
	// doc-example:end tracking-url-codec
	return codec
}
