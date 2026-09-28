package httpdto

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"strings"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// StreamLabel owns its private representation and native streaming wire methods.
type StreamLabel struct{ text string }

func NewStreamLabel(text string) StreamLabel { return StreamLabel{text: text} }
func (v StreamLabel) Text() string           { return v.text }
func (v StreamLabel) MarshalJSONTo(enc *jsontext.Encoder) error {
	return enc.WriteToken(jsontext.String("label:" + v.text))
}
func (v *StreamLabel) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	var text string
	if err := jsonv2.UnmarshalDecode(dec, &text); err != nil {
		return err
	}
	text, ok := strings.CutPrefix(text, "label:")
	if !ok {
		return fault.New(fault.Invalid, "invalid label")
	}
	*v = NewStreamLabel(text)
	return nil
}
func (StreamLabel) JSONContract() contract.JSON[StreamLabel] {
	return contract.ScalarJSON(contract.DefineScalar[StreamLabel](contract.Type{
		ID: "foundry.test/consumer/httpdto.StreamLabel", Kind: contract.StringKind,
	}))
}

//foundry:dto
type StreamResponse struct {
	Label   StreamLabel
	History []StreamLabel
	Next    value.Optional[value.Nullable[StreamLabel]] `json:"Next,omitzero"`
}

var StreamLabelContract = StreamLabel{}.JSONContract()
