package ormquerytest

import "github.com/weiloon1234/Foundry-Go/database/orm"

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
	orm.Table[uuidRecord, model.ID[uuidRecord]]
	Other orm.NullableColumn[uuidRecord, model.ID[uuidRecord]]
}

func uuidFields() uuidTable {
	return orm.Define("uuid_records", func(b orm.Builder[uuidRecord], r *uuidRecord) uuidTable {
		return uuidTable{orm.TableOf(b, &r.ID), orm.NullableIDOf(b, &r.Other)}
	})
}
func TestUUIDComparisonDeclarationOwnership(t *testing.T) {
	d, foreign := uuidFields(), uuidFields()
	id := model.ID[uuidRecord]{}
	for _, p := range []orm.Predicate[uuidRecord]{orm.UUIDGreater(foreign.Key(), id), orm.UUIDLess(orm.Key[uuidRecord, model.ID[uuidRecord]]{}, id), orm.UUIDGreater(orm.NullableColumn[uuidRecord, model.ID[uuidRecord]]{}, id)} {
		if _, err := orm.Compile(d.Query().Where(p)); err == nil {
			t.Fatal("foreign/zero UUID comparison accepted")
		}
	}
	for _, p := range []orm.Predicate[uuidRecord]{orm.UUIDLess(d.Key(), id), orm.UUIDLessOrEqual(d.Other, id), orm.UUIDGreater(d.Key(), id), orm.UUIDGreaterOrEqual(d.Other, id)} {
		s, err := orm.Compile(d.Query().Where(p))
		if err != nil || !strings.Contains(s.SQL(), "$1") {
			t.Fatal(s, err)
		}
	}
}
