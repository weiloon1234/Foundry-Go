package isolatedhttp

import (
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func TestORMDeclarationsPreserveStoredQuery(t *testing.T) {
	if err := orm.Check(Tables()...); err != nil {
		t.Fatal(err)
	}
	prior, err := QueryScopeRecords().Where(RecordFields().Name.Eq("fixture")).Compile()
	if err != nil {
		t.Fatal(err)
	}
	current, err := orm.Compile(RecordRows.Query().Where(RecordRows.Name.Eq("fixture")))
	if err != nil {
		t.Fatal(err)
	}
	if prior.SQL() != current.SQL() || !reflect.DeepEqual(prior.Arguments(), current.Arguments()) {
		t.Fatal("stored query changed", prior.SQL(), current.SQL())
	}
}
