package pagination_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/contract"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestPaginationSchemaIsOwnedDeterministicAndMatchesWireShape(t *testing.T) {
	t.Parallel()
	descriptor := pagination.NumberedJSON(itemJSON())
	original, err := descriptor.Description()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		again, err := pagination.NumberedJSON(itemJSON()).Description()
		if err != nil {
			t.Fatal(err)
		}
		wire, err := json.Marshal(again)
		if err != nil || !bytes.Equal(encoded, wire) {
			t.Fatal("nondeterministic page graph")
		}
	}
	original.Types[0].ID = "changed"
	unchanged, _ := descriptor.Description()
	if unchanged.Types[0].ID == "changed" {
		t.Fatal("metadata aliases descriptor")
	}
	limits := foundryhttp.DefaultEndpointLimits().Response
	for _, body := range []string{
		`{"data":null,"meta":{"current_page":1,"per_page":20,"total":0,"last_page":0},"links":{"next":null,"prev":null}}`,
		`{"data":[],"meta":{"current_page":1,"per_page":20,"total":"0","last_page":0},"links":{"next":null,"prev":null}}`,
		`{"data":[{"label":1}],"meta":{"current_page":1,"per_page":20,"total":1,"last_page":1},"links":{"next":null,"prev":null}}`,
	} {
		if _, err := descriptor.Decode(t.Context(), []byte(body), limits); err == nil {
			t.Fatal("invalid page shape accepted", body)
		}
	}
}

type PrivateModel struct{ ID model.ID[PrivateModel] }

func (PrivateModel) FoundryIdentity() (model.Identity, error) { return model.Identity{}, nil }
func TestPageJSONCannotMakeModelPublic(t *testing.T) {
	typ := reflect.TypeFor[PrivateModel]()
	id := contract.TypeID(typ.PkgPath() + "." + typ.Name())
	invalid := contract.DefineJSON[PrivateModel](contract.Schema{Root: id, Types: []contract.Type{{ID: id, Kind: contract.ObjectKind}}})
	if pagination.NumberedJSON(invalid).Validate() == nil {
		t.Fatal("model became a public DTO")
	}
}
