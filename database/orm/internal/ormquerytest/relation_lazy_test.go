package ormquerytest

import "github.com/weiloon1234/Foundry-Go/database/orm"

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
		orm.Table[target, int64]
		OwnerID orm.Number[target, int64]
	}
	type parentFields struct {
		orm.Table[parent, int64]
		Children orm.Many[parent, target]
	}
	relationEnumCalls = 0
	children := orm.Define("lazy_children", func(b orm.Builder[target], m *target) targetFields {
		orm.EnumOf(b, &m.State)
		return targetFields{orm.TableOf(b, &m.ID), orm.NumberOf(b, &m.OwnerID)}
	})
	parents := orm.Define("lazy_parents", func(b orm.Builder[parent], m *parent) parentFields {
		return parentFields{orm.TableOf(b, &m.ID), orm.ManyOf(b, &m.Children)}
	})
	r := orm.HasManyOf(parents.Children, orm.KeyExpr(parents.Key()), orm.NumberExpr(children.OwnerID))
	q := parents.Query().Where(orm.Has(r))
	if relationEnumCalls != 0 {
		t.Fatal("relation construction resolved enum descriptor")
	}
	var statements []string
	db := testDB(t, testConnector{statements: &statements})
	if _, err := orm.Count(t.Context(), db, q); !errors.Is(err, fault.Invalid) {
		t.Fatal("target codec not checked", err)
	}
	if len(statements) != 0 || relationEnumCalls != 1 {
		t.Fatal("invalid target sent SQL or did not validate", statements, relationEnumCalls)
	}
}
