package orm

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/enum"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type badRelationEnum string

var relationEnumCalls int

func (badRelationEnum) EnumDescriptor() enum.Descriptor[badRelationEnum] {
	relationEnumCalls++
	return enum.Descriptor[badRelationEnum]{}
}

func TestRelationExistenceDefersTargetEnumChecks(t *testing.T) {
	type target struct {
		ID      int64
		OwnerID int64
		State   badRelationEnum
	}
	type parent struct {
		ID       int64
		Children relation.Many[target]
	}
	type targetFields struct {
		Table[target, int64]
		OwnerID Number[target, int64]
	}
	type parentFields struct {
		Table[parent, int64]
		Children Many[parent, target]
	}
	relationEnumCalls = 0
	children := Define("lazy_children", func(b Builder[target], m *target) targetFields {
		EnumOf(b, &m.State)
		return targetFields{TableOf(b, &m.ID), NumberOf(b, &m.OwnerID)}
	})
	parents := Define("lazy_parents", func(b Builder[parent], m *parent) parentFields {
		return parentFields{TableOf(b, &m.ID), ManyOf(b, &m.Children)}
	})
	r := HasManyOf(parents.Children, KeyExpr(parents.Key()), NumberExpr(children.OwnerID))
	q := parents.Query().Where(Has(r))
	if relationEnumCalls != 0 {
		t.Fatal("relation construction resolved enum descriptor")
	}
	var statements []string
	db := testDB(t, testConnector{statements: &statements})
	if _, err := Count(t.Context(), db, q); !errors.Is(err, fault.Invalid) {
		t.Fatal("target codec not checked", err)
	}
	if len(statements) != 0 || relationEnumCalls != 1 {
		t.Fatal("invalid target sent SQL or did not validate", statements, relationEnumCalls)
	}
}
