package embeddedvar

import (
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

// SKU is a counterexample fixture: a codec declares Parse rejecting "",
// inherits Format from an embedded formatter, and is registered through a
// package-level var using DescribeURL and DefineScalar without NonEmpty.
type SKU string

type skuFormatter struct{}

func (skuFormatter) Format(v SKU) (string, error) { return string(v), nil }

type skuCodec struct{ skuFormatter }

func (skuCodec) Parse(text string) (SKU, error) {
	if text == "" {
		return "", safeerror.NewFault(fault.Invalid, "empty value")
	}
	return SKU(text), nil
}

var SKUURL = foundryhttp.DescribeURL(
	skuCodec{},
	contract.DefineScalar[SKU](contract.Type{ID: "embeddedvar.SKU", Kind: contract.StringKind}),
)
