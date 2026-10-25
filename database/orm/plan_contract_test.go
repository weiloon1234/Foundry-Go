package orm_test

import (
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func TestPlanContractsShareOneOwner(t *testing.T) {
	for _, types := range [][2]reflect.Type{
		{reflect.TypeFor[orm.Plan](), reflect.TypeFor[querycore.Plan]()},
		{reflect.TypeFor[orm.PlanNode](), reflect.TypeFor[querycore.PlanNode]()},
	} {
		if types[0] != types[1] {
			t.Fatalf("plan aliases have different owners: %v", types)
		}
	}
}
