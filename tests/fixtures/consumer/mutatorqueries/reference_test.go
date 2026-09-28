package mutatorqueries_test

import (
	"encoding/json"
	"strings"
	"testing"

	"foundry.test/consumer/linkqueries"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestGeneratedReferencesPreserveStoredKeysAndCaptureOnlyProvenance(t *testing.T) {
	id, err := model.NewID[mutatorqueries.Member]()
	if err != nil {
		t.Fatal(err)
	}
	member := mutatorqueries.Member{ID: id, Email: "private-email@example.test"}
	reference := member.FoundryReference()
	var stored model.ID[mutatorqueries.Member] = reference.Key()
	if stored != id {
		t.Fatal("typed reference replaced stored ID")
	}
	identity, err := member.FoundryIdentity()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := (mutatorqueries.Member{}).FoundryReference().Parse(identity)
	if err != nil || restored.Key() != id {
		t.Fatal("generated identity round trip failed", err)
	}
	origin, err := (attribution.Origin{}).WithModel(member)
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := attribution.WithContext(t.Context(), origin)
	if err != nil {
		t.Fatal(err)
	}
	member.ID = model.ID[mutatorqueries.Member]{}
	captured, ok := attribution.FromContext(ctx).Model()
	if !ok || captured != identity {
		t.Fatal("attribution changed with model")
	}
	data, err := json.Marshal(origin)
	if err != nil || strings.Contains(string(data), member.Email) {
		t.Fatal("attribution copied unrelated model data", err)
	}
	group := linkqueries.Group{Code: " untouched "}
	natural, err := group.FoundryIdentity()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := (linkqueries.Group{}).FoundryReference().Parse(natural)
	var code linkqueries.GroupCode = decoded.Key()
	if err != nil || code != group.Code {
		t.Fatal("natural key changed", err)
	}
}
