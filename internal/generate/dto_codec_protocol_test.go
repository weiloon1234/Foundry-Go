package generate

import (
	"strings"
	"testing"
)

func TestDTORequiresExplicitContractForNativeCodecProtocols(t *testing.T) {
	for _, source := range []string{
		`import "encoding/json/jsontext"
         type Custom struct { Label string }
         func (Custom) MarshalJSONTo(*jsontext.Encoder) error { return nil }`,
		`import "encoding/json/jsontext"
         type Custom struct { Label string }
         func (*Custom) UnmarshalJSONFrom(*jsontext.Decoder) error { return nil }`,
		`type Custom string
         func (Custom) AppendText(b []byte) ([]byte, error) { return append(b, "custom"...), nil }`,
	} {
		dir := fixture(t, "package sample\n"+source+"\n//foundry:dto\ntype Response struct { Value Custom }\n")
		_, err := Generate(t.Context(), Options{Dir: dir})
		if err == nil || !strings.Contains(err.Error(), "explicit transport contract") {
			t.Fatalf("custom codec protocol was inferred as Go fields: %v", err)
		}
		if len(generatedSnapshot(t, dir)) != 0 {
			t.Fatal("rejected codec wrote generated output")
		}
	}
}

func TestDTODoesNotMistakeUnrelatedMethodsForCodecProtocols(t *testing.T) {
	dir := fixture(t, `package sample
import "strings"
type Custom struct { Label string }
func (Custom) MarshalJSONTo(*strings.Builder) error { return nil }
func (Custom) AppendText(string) (string,error) { return "",nil }
//foundry:dto
type Response struct { Value Custom }
`)
	if _, err := Generate(t.Context(), Options{Dir: dir}); err != nil {
		t.Fatalf("unrelated methods were treated as JSON codecs: %v", err)
	}
}
