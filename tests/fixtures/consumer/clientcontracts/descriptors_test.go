package clientcontracts_test

import (
	"encoding/json"
	"math"
	"path/filepath"
	"testing"

	"foundry.test/consumer/internal/clientfixture"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
)

func TestClientDescriptorsPreserveExtremeMetadata(t *testing.T) {
	tools := clientfixture.Load(t)
	source, err := fixture(t).Manifest(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	doc, err := source.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the saved-manifest boundary with a valid platform-sized limit
	// beyond JavaScript's safe integer range. No request uses this test policy.
	for i := range doc.HTTP {
		if doc.HTTP[i].Name == "itemsEcho" {
			doc.HTTP[i].Limits.Body.Bytes = math.MaxInt
		}
	}
	doc.Types = append(doc.Types,
		contract.Type{ID: "client.ExactEnum", Kind: contract.IntegerKind, Bits: 64, Signed: true, Cases: []json.RawMessage{json.RawMessage("9223372036854775807")}},
		contract.Type{ID: "client.QuotedExactEnum", Kind: contract.QuotedKind, Element: "client.ExactEnum"},
		contract.Type{ID: "client.NullableEnum", Kind: contract.AliasKind, Element: "client.ExactEnum", Nullable: true},
		contract.Type{ID: "client.RequiredEnum", Kind: contract.AliasKind, Element: "client.NullableEnum"},
	)
	doc.Roots = append(doc.Roots, "client.ExactEnum", "client.QuotedExactEnum", "client.NullableEnum", "client.RequiredEnum")
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	source, err = manifest.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	tools.Check(t, source, "", filepath.Join("testdata", "descriptors"))
}
