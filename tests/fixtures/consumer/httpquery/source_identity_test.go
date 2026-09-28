package httpquery_test

import (
	"testing"

	"foundry.test/consumer/httpquery"
	"github.com/weiloon1234/Foundry-Go/contract"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
)

func TestGeneratedURLAndDTOShareScalarIdentity(t *testing.T) {
	const expected contract.TypeID = "foundry.test/consumer/httpquery.Priority"
	path, err := httpquery.PriorityPathDescriptor().Parameters()
	if err != nil || len(path) != 1 || path[0].Scalar == nil || path[0].Scalar.Value.ID != expected {
		t.Fatal("path source identity", err)
	}
	query, err := httpquery.PriorityQueryDescriptor().Parameters()
	if err != nil || len(query) != 1 || query[0].Scalar == nil || query[0].Scalar.Value.ID != expected {
		t.Fatal("query source identity", err)
	}
	schema, err := httpquery.PriorityResponseJSON().Description()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, typ := range schema.Types {
		if typ.ID == expected {
			found = true
			if typ.Kind != contract.IntegerKind || typ.Bits != 8 || typ.Signed {
				t.Fatal("source shape changed")
			}
		}
	}
	if !found {
		t.Fatal("DTO scalar source missing")
	}
	request, err := httpquery.PriorityQueryDescriptor().Decode(t.Context(), "priority=255", foundryhttp.QueryLimits{Bytes: 128, Pairs: 4, Issues: 4})
	if err != nil || request.Priority != 255 {
		t.Fatal("priority decode", err)
	}
	if _, err := httpquery.PriorityQueryDescriptor().Decode(t.Context(), "priority=256", foundryhttp.QueryLimits{Bytes: 128, Pairs: 4, Issues: 4}); err == nil {
		t.Fatal("priority width not preserved")
	}
}
