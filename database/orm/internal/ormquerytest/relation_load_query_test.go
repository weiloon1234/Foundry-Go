package ormquerytest

import "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestLoadFromQueryLimitsAndStatementContract(t *testing.T) {
	p := people()
	rel := orm.HasManyOf(p.Reports, orm.KeyExpr(p.ID), orm.NullableNumberExpr(p.ManagerID))
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "refresh", true: "missing"}[missing], func(t *testing.T) {
			run := orm.LoadFrom[relationPerson]
			if missing {
				run = orm.LoadMissingFrom[relationPerson]
			}
			for _, budget := range []int{2, 3, 5} {
				o := treeOracle()
				db := testDB(t, o.connector())
				parents := []relationPerson{{ID: 1}, {ID: 2}}
				// Parent clauses do not filter, reorder or window supplied models.
				q := p.Query().Where(orm.KeyEq(p.ID, 999)).OrderBy(orm.KeyDesc(p.ID)).Limit(0).With(rel.With(rel))
				q = orm.WithRelationLimits(q, orm.RelationLimits{BatchSize: 1, MaxRows: budget, MaxDepth: 2})
				got, err := run(t.Context(), db, q, parents)
				if budget < 5 {
					if got != nil || !errors.Is(err, fault.Invalid) {
						t.Fatal("published partial load", got, err)
					}
				} else {
					if err != nil || len(got) != 2 || got[0].ID != 1 || got[1].ID != 2 || got[0].Reports.Len() != 2 || got[1].Reports.Len() != 1 {
						t.Fatal("parent collection or related rows changed", got, err)
					}
					if o.calls != 5 {
						t.Fatal("expected two root batches plus three nested batches, no parent statement", o.calls)
					}
					for _, parent := range got {
						for _, child := range parent.Reports.All() {
							if !child.Reports.IsLoaded() {
								t.Fatal("nested branch missing")
							}
						}
					}
				}
				if parents[0].Reports.IsLoaded() || parents[1].Reports.IsLoaded() || o.closed != o.calls {
					t.Fatal("input mutated or rows left open")
				}
			}
			o := treeOracle()
			db := testDB(t, o.connector())
			parents := []relationPerson{{ID: 1, Reports: relation.Collection([]relationPerson{{ID: 99}})}, {ID: 2}}
			q := orm.WithRelationLimits(p.Query(), orm.RelationLimits{BatchSize: 1, MaxRows: 1, MaxDepth: 1}).With(rel)
			got, err := run(t.Context(), db, q, parents)
			if missing {
				if err != nil || len(got) != 2 || got[1].Reports.Len() != 1 || o.calls != 1 {
					t.Fatal("loaded slot or missing-only budget changed", got, err, o.calls)
				}
				preserved, _ := got[0].Reports.Get()
				if !reflect.DeepEqual(preserved, []relationPerson{{ID: 99}}) {
					t.Fatal("loaded slot overwritten")
				}
				before := o.calls
				if _, err = run(t.Context(), db, q, got); err != nil || o.calls != before {
					t.Fatal("already loaded slots queried", err)
				}
			} else if got != nil || !errors.Is(err, fault.Invalid) {
				t.Fatal("refresh ignored budget", got, err)
			}
			preserved, _ := parents[0].Reports.Get()
			if !reflect.DeepEqual(preserved, []relationPerson{{ID: 99}}) || parents[1].Reports.IsLoaded() {
				t.Fatal("caller-owned slots changed")
			}
		})
	}
}

func TestLoadFromQueryValidatesDepthAndDeclarationBeforeSQL(t *testing.T) {
	p, other := people(), people()
	rel := orm.HasManyOf(p.Reports, orm.KeyExpr(p.ID), orm.NullableNumberExpr(p.ManagerID))
	for _, run := range []func(context.Context, database.Executor, orm.Query[relationPerson], []relationPerson) ([]relationPerson, error){orm.LoadFrom[relationPerson], orm.LoadMissingFrom[relationPerson]} {
		o := treeOracle()
		db := testDB(t, o.connector())
		for _, q := range []orm.Query[relationPerson]{
			{},
			orm.WithRelationLimits(p.Query().With(rel.With(rel)), orm.RelationLimits{BatchSize: 1, MaxRows: 10, MaxDepth: 1}),
			orm.WithRelationLimits(p.Query(), orm.RelationLimits{}),
			other.Query().With(rel),
			p.Query().Where(orm.KeyEq(other.ID, 1)).With(rel),
		} {
			got, err := run(t.Context(), db, q, []relationPerson{{ID: 1}})
			if got != nil || !errors.Is(err, fault.Invalid) || o.calls != 0 {
				t.Fatal("invalid query performed SQL", got, err, o.calls)
			}
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if got, err := run(ctx, db, p.Query().With(rel), []relationPerson{{ID: 1}}); got != nil || !errors.Is(err, context.Canceled) || o.calls != 0 {
			t.Fatal("cancellation lost", err)
		}
	}
}
