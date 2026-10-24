package orm

import (
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type uuidRecord struct {
	ID    model.ID[uuidRecord]
	Other value.Nullable[model.ID[uuidRecord]]
}
type uuidTable struct {
	Table[uuidRecord, model.ID[uuidRecord]]
	Other NullableColumn[uuidRecord, model.ID[uuidRecord]]
}

func uuidFields() uuidTable {
	return Define("uuid_records", func(b Builder[uuidRecord], r *uuidRecord) uuidTable {
		return uuidTable{TableOf(b, &r.ID), NullableIDOf(b, &r.Other)}
	})
}
func TestUUIDComparisonDeclarationOwnership(t *testing.T) {
	d, foreign := uuidFields(), uuidFields()
	id := model.ID[uuidRecord]{}
	for _, p := range []Predicate[uuidRecord]{UUIDGreater(foreign.Key(), id), UUIDLess(Key[uuidRecord, model.ID[uuidRecord]]{}, id), UUIDGreater[uuidRecord, uuidRecord](nil, id)} {
		if _, err := Compile(d.Query().Where(p)); err == nil {
			t.Fatal("foreign/zero UUID comparison accepted")
		}
	}
	for _, p := range []Predicate[uuidRecord]{UUIDLess(d.Key(), id), UUIDLessOrEqual(d.Other, id), UUIDGreater(d.Key(), id), UUIDGreaterOrEqual(d.Other, id)} {
		s, err := Compile(d.Query().Where(p))
		if err != nil || !strings.Contains(s.SQL(), "$1") {
			t.Fatal(s, err)
		}
	}
}
