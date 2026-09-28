package httpquery

import (
	"strings"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/value"
)

// TrackingCode is an application-owned text protocol. The scalar declaration
// describes its wire value; the methods own its domain representation checks.
type TrackingCode string

func (v TrackingCode) MarshalText() ([]byte, error) {
	if !strings.HasPrefix(string(v), "track_") {
		return nil, fault.New(fault.Invalid, "invalid tracking code")
	}
	return []byte(v), nil
}
func (v *TrackingCode) UnmarshalText(data []byte) error {
	candidate := TrackingCode(data)
	if _, err := candidate.MarshalText(); err != nil {
		return err
	}
	*v = candidate
	return nil
}

var TrackingScalar = contract.DefineScalar[TrackingCode](contract.Type{
	ID: "foundry.test/consumer/httpquery.TrackingCode", Kind: contract.StringKind,
})

type TrackingInput struct{ Code value.Optional[TrackingCode] }

func TrackingParameters() foundryhttp.Query[TrackingInput] {
	codec := foundryhttp.DescribeURL(foundryhttp.TextQuery[TrackingCode, *TrackingCode](), TrackingScalar)
	return foundryhttp.DefineQuery(foundryhttp.OptionalQueryParam("tracking", codec, func(input *TrackingInput) *value.Optional[TrackingCode] { return &input.Code }))
}

func TrackingDescription() (contract.Type, error) { return TrackingScalar.Description() }
