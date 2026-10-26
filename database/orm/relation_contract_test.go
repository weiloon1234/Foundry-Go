package orm_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestRelationContractsShareOneOwner(t *testing.T) {
	for _, tc := range []struct {
		native, legacy, owner reflect.Type
		path                  string
	}{
		{reflect.TypeFor[orm.MorphName](), reflect.TypeFor[query.MorphName](), reflect.TypeFor[model.MorphName](), "github.com/weiloon1234/Foundry-Go/model"},
		{reflect.TypeFor[orm.Morphable](), reflect.TypeFor[query.Morphable](), reflect.TypeFor[model.Morphable](), "github.com/weiloon1234/Foundry-Go/model"},
		{reflect.TypeFor[orm.RelationLimits](), reflect.TypeFor[query.RelationLimits](), reflect.TypeFor[querycore.RelationLimits](), "github.com/weiloon1234/Foundry-Go/internal/querycore"},
	} {
		if tc.native != tc.owner || tc.legacy != tc.owner || tc.owner.PkgPath() != tc.path {
			t.Fatalf("relation contract has multiple owners: %v", tc)
		}
	}
	if orm.DefaultRelationLimits() != querycore.DefaultRelationLimits() || query.DefaultRelationLimits() != querycore.DefaultRelationLimits() {
		t.Fatal("relation defaults diverged")
	}
	for _, limits := range []orm.RelationLimits{
		{}, {BatchSize: 1001, MaxRows: 1, MaxDepth: 1}, {BatchSize: 1, MaxRows: 1000001, MaxDepth: 1}, {BatchSize: 1, MaxRows: 1, MaxDepth: 33},
	} {
		err := limits.Validate()
		if !errors.Is(err, fault.Invalid) || err.Error() != "invalid: invalid relation resource limits" {
			t.Fatalf("bounds or diagnostic changed: %v", err)
		}
	}
	for _, limits := range []orm.RelationLimits{{BatchSize: 1, MaxRows: 1, MaxDepth: 1}, {BatchSize: 1000, MaxRows: 1000000, MaxDepth: 32}} {
		if err := limits.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		input any
		want  string
	}{
		{model.MorphName("post"), `"post"`},
		{orm.DefaultRelationLimits(), `{"BatchSize":500,"MaxRows":10000,"MaxDepth":8}`},
	} {
		got, err := json.Marshal(tc.input)
		if err != nil || string(got) != tc.want {
			t.Fatalf("relation bytes changed: %s, %v", got, err)
		}
	}
}
