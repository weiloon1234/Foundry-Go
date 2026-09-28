package settings

import (
	"encoding/json"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestTypedPresentationValidation(t *testing.T) {
	for _, kind := range []Kind{Text, Textarea, Number, Boolean, Select, Multiselect, Email, URL, Color, Date, Datetime, File, Image, JSON, Password, Code} {
		if err := Define("site.name", 1, contract.StringJSON[string](), Presentation{Kind: kind}).Validate(); err != nil {
			t.Fatal(kind, err)
		}
	}
	for _, key := range []Key[string]{Define("bad name", 1, contract.StringJSON[string](), Presentation{}), Define("name", 0, contract.StringJSON[string](), Presentation{}), Define("name", 1, contract.JSON[string]{}, Presentation{}), Define("name", 1, contract.StringJSON[string](), Presentation{Kind: "unknown"}), Define("name", 1, contract.StringJSON[string](), Presentation{Label: "null\x00byte"})} {
		if key.Validate() == nil {
			t.Fatal("invalid setting descriptor accepted")
		}
	}
	parameters, err := value.ParseJSON[json.RawMessage](`[1,2]`)
	if err != nil {
		t.Fatal(err)
	}
	if Define("name", 1, contract.StringJSON[string](), Presentation{Parameters: parameters}).Validate() == nil {
		t.Fatal("non-object widget parameters")
	}
}
