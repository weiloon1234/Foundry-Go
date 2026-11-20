package goway_test

import (
	"errors"
	"strings"
	"testing"

	"foundry.test/goway/models"
	"foundry.test/goway/models/mutators"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	rowset "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestReportTextSupportedFamilies(t *testing.T) {
	ordinary := rowset.FromModel(models.Users.Query()).Where(expr.Gt(expr.TextRow(models.Users.Email), "ada@example.test"), expr.NullableTextContains(expr.TextNullableRow(models.Users.Nickname), "Ada"))
	if _, err := rowset.CompileRead(rowset.Read(ordinary)); err != nil {
		t.Fatal(err)
	}
	input := rowset.FromModel(mutators.Members.Query()).Where(expr.Gt(expr.TextRow(mutators.Members.Email), "ada@example.test"), expr.NullableTextContains(expr.TextNullableRow(mutators.Members.Note), "Ada"))
	if _, err := rowset.CompileRead(rowset.Read(input)); err != nil {
		t.Fatal(err)
	}
}

// A shallower Relation can similarly override the plan while a donor supplies
// a different private target-model marker.
type reportRelationDonor struct {
	orm.BelongsTo[models.User, models.User]
}
type reportForgedRelation struct {
	orm.Relation[models.User]
	reportRelationDonor
}

func TestReportRelationNestedTargetMarker(t *testing.T) {
	r := reportForgedRelation{Relation: models.UserRelations.Orders}
	q := rowset.FromModel(models.Users.Query())
	for _, ps := range [][]expr.Predicate[models.User]{nil, {expr.ModelPredicate(orm.WhereRaw[models.User]("TRUE"))}} {
		_, err := rowset.CompileRead(rowset.Read(q.Where(expr.Has[models.User, models.User](r, ps...))))
		if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "relation model types do not match bound metadata") {
			t.Fatalf("promoted relation target accepted: %v", err)
		}
	}
	// Embedding does not itself invalidate a relation when its own plan agrees.
	r.Relation = models.UserRelations.Introducer
	if _, err := rowset.CompileRead(rowset.Read(q.Where(expr.Has[models.User, models.User](r)))); err != nil {
		t.Fatal(err)
	}
}

func TestReportModelPredicateNestedRelationTargetMarker(t *testing.T) {
	q := rowset.FromModel(models.Users.Query())
	for _, matching := range []bool{false, true} {
		r := reportForgedRelation{Relation: models.UserRelations.Orders}
		if matching {
			r.Relation = models.UserRelations.Introducer
		}
		for _, relation := range []orm.ExistenceRelation[models.User, models.User]{r, &r} {
			for name, ps := range map[string][]orm.Predicate[models.User]{
				"no child":             nil,
				"reference-free child": {orm.WhereRaw[models.User]("TRUE")},
				"model child":          {orm.Gt(models.Users.Age, 0)},
			} {
				for operation, predicate := range map[string]orm.Predicate[models.User]{
					"Has":        orm.Has[models.User, models.User](relation, ps...),
					"DoesntHave": orm.Not(orm.Has(relation, ps...)),
				} {
					_, err := rowset.CompileRead(rowset.Read(q.Where(expr.ModelPredicate(predicate))))
					if matching {
						if err != nil {
							t.Fatalf("matching %s (%s): %v", operation, name, err)
						}
					} else if !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "relation model types do not match bound metadata") {
						t.Fatalf("forged %s (%s) accepted: %v", operation, name, err)
					}
				}
			}
		}
	}
}
