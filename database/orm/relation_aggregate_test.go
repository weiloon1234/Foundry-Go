package orm

import (
	"database/sql/driver"
	"errors"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

type aggregateModel struct {
	ID       int64
	ParentID int64
	Children relation.Many[aggregateModel]
	Count    relation.Value[int64]
	Links    relation.Through[aggregateModel, aggregateModel]
}
type aggregateTable struct {
	Table[aggregateModel, int64]
	ParentID Number[aggregateModel, int64]
	Children Many[aggregateModel, aggregateModel]
	Count    Value[aggregateModel, int64]
	Links    Through[aggregateModel, aggregateModel, aggregateModel]
}

func aggregateModels() aggregateTable {
	return Define("aggregate_models", func(b Builder[aggregateModel], m *aggregateModel) aggregateTable {
		return aggregateTable{TableOf(b, &m.ID), NumberOf(b, &m.ParentID), ManyOf(b, &m.Children), ValueOf(b, &m.Count), ThroughOf(b, &m.Links)}
	})
}
func aggregateCount(source AggregateSource[aggregateModel, aggregateModel], seal sqlowner.ExpressionSeal) AggregateRelation[aggregateModel, int64] {
	return FoundryRelated(source, ormcore.Expression{Node: querycore.AggregateNode{Kind: querycore.CountAll}, Refs: []ormcore.Reference{{Scope: reflect.TypeFor[aggregateModel]()}}, ReferenceFree: true}, ormcore.Column[int64]{Scan: func(v *int64) any { return codec.Signed[int64]().Scan(v) }}, seal)
}

func TestAggregateCapabilityAndDeclarationIdentity(t *testing.T) {
	a, b := aggregateModels(), aggregateModels()
	source := HasManyOf(a.Children, KeyExpr(a.Key()), NumberExpr(a.ParentID))
	valid := a.Count.Using(aggregateCount(source, sqlowner.Expressions()))
	if err := Check(a, valid); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []CheckedTable{
		a.Count.Using(aggregateCount(source, sqlowner.ExpressionSeal{})),
		b.Count.Using(aggregateCount(source, sqlowner.Expressions())),
		a.Count.Using(aggregateCount(nil, sqlowner.Expressions())),
		Value[aggregateModel, int64]{}.Using(aggregateCount(source, sqlowner.Expressions())),
	} {
		if err := Check(bad); !errors.Is(err, fault.Invalid) {
			t.Fatal("bad capability/binding accepted", err)
		}
	}
	column, _ := FoundryColumn(NumberExpr(b.ParentID), sqlowner.Metadata())
	wrong := FoundryRelated(source, ormcore.Expression{Node: querycore.AggregateNode{Kind: querycore.CountValues, Field: column.Field}, Refs: []ormcore.Reference{column.Reference}}, ormcore.Column[int64]{Scan: func(v *int64) any { return codec.Signed[int64]().Scan(v) }}, sqlowner.Expressions())
	if err := Check(a.Count.Using(wrong)); !errors.Is(err, fault.Invalid) {
		t.Fatal("same Go type/different declaration accepted", err)
	}
	q := b.Query().With(valid)
	if err := validateQuery(q.state); !errors.Is(err, fault.Invalid) {
		t.Fatal("query declaration mismatch", err)
	}
}

func TestAggregateUsingKeepsSlotAndSnapshots(t *testing.T) {
	a := aggregateModels()
	source := HasManyOf(a.Children, KeyExpr(a.Key()), NumberExpr(a.ParentID))
	base := a.Count.Using(aggregateCount(source, sqlowner.Expressions()))
	derived := base.Using(aggregateCount(source.Where(NumberGt(a.ParentID, 1)), sqlowner.Expressions()))
	if base.plan.slot != derived.plan.slot || len(base.plan.aggregate.input.plan.query.ast.Predicates) != 0 || len(derived.plan.aggregate.input.plan.query.ast.Predicates) != 1 {
		t.Fatal("Using mutated source or changed destination")
	}
	var statements []string
	db := testDB(t, testConnector{statements: &statements, columns: []string{"key", "count"}, values: [][]driver.Value{{int64(1), int64(2)}}})
	input := []aggregateModel{{ID: 1}}
	out, err := LoadFrom(t.Context(), db, a.Query().With(derived), input)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := out[0].Count.Get(); !ok || v != 2 || input[0].Count.IsLoaded() {
		t.Fatal("attachment ownership")
	}
	if len(statements) != 1 {
		t.Fatal("statement count", len(statements))
	}
}

func TestAggregateSlotDeclarationErrors(t *testing.T) {
	t.Run("duplicate", func(t *testing.T) {
		bad := Define("aggregate_models", func(b Builder[aggregateModel], m *aggregateModel) aggregateTable {
			a := aggregateTable{TableOf(b, &m.ID), NumberOf(b, &m.ParentID), ManyOf(b, &m.Children), ValueOf(b, &m.Count), ThroughOf(b, &m.Links)}
			ValueOf(b, &m.Count)
			return a
		})
		if err := Check(bad); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	})
	t.Run("foreign_pointer", func(t *testing.T) {
		bad := Define("aggregate_models", func(b Builder[aggregateModel], m *aggregateModel) aggregateTable {
			return aggregateTable{TableOf(b, &m.ID), NumberOf(b, &m.ParentID), ManyOf(b, &m.Children), ValueOf(b, &new(aggregateModel).Count), ThroughOf(b, &m.Links)}
		})
		if err := Check(bad); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	})
}

func TestAggregateThroughKeepsInputPredicates(t *testing.T) {
	a := aggregateModels()
	source := HasManyThroughOf(a.Children, KeyExpr(a.Key()), NumberExpr(a.ParentID), KeyExpr(a.Key()), NumberExpr(a.ParentID)).Where(NumberGt(a.ParentID, 0))
	before := source.plan.query.ast.Predicates[0].(querycore.Comparison)
	r := a.Count.Using(aggregateCount(source, sqlowner.Expressions()))
	for range 2 {
		if err := Check(r, source); err != nil {
			t.Fatal(err)
		}
	}
	if after := source.plan.query.ast.Predicates[0].(querycore.Comparison); !reflect.DeepEqual(before.Operand, after.Operand) || before.Operator != after.Operator || !reflect.DeepEqual(before.Values, after.Values) {
		t.Fatal("aggregate aliasing mutated the input relation")
	}
}

type aggregateMetadataSource struct {
	plan  *relationPlan
	input aggregateInput
}

func (s aggregateMetadataSource) aggregateInput(*aggregateModel, *aggregateModel) aggregateInput {
	return s.input
}

func TestAggregateSourceMetadataIdentity(t *testing.T) {
	a := aggregateModels()
	native := HasManyOf(a.Children, KeyExpr(a.Key()), NumberExpr(a.ParentID))
	source := aggregateMetadataSource{plan: native.plan, input: native.aggregateInput(nil, nil)}
	source.input.identity = reflect.TypeFor[aggregateMetadataSource]()
	for _, change := range []func(*aggregateInput){
		func(input *aggregateInput) { input.capability = sqlowner.MetadataSeal{} },
		func(input *aggregateInput) {
			input.identity = reflect.TypeFor[HasOne[aggregateModel, aggregateModel]]()
		},
		func(input *aggregateInput) { input.plan = copyRelationPlan(native.plan) },
		func(input *aggregateInput) { input.plan = nil },
	} {
		bad := source
		change(&bad.input)
		if err := Check(a.Count.Using(aggregateCount(bad, sqlowner.Expressions()))); !errors.Is(err, fault.Invalid) {
			t.Fatal("foreign source metadata accepted", err)
		}
	}
}

func TestAggregateValidationAndCapturedScopes(t *testing.T) {
	a := aggregateModels()
	r := ManyToManyOf(a.Links, KeyExpr(a.Key()), NumberExpr(a.ParentID), KeyExpr(a.Key()), KeyExpr(a.Key()))
	child := HasManyOf(a.Children, KeyExpr(a.Key()), NumberExpr(a.ParentID))
	count := a.Count.Using(aggregateCount(r, sqlowner.Expressions()))
	if err := validateQuery(a.Query().With(count).state); err != nil {
		t.Fatal(err)
	}
	var missing *ThroughRelation[aggregateModel, aggregateModel, aggregateModel]
	for _, bad := range []Query[aggregateModel]{
		a.Query().With(count, count),
		a.Query().With(a.Count.Using(aggregateCount(missing, sqlowner.Expressions()))),
		a.Query().With(a.Count.Using(AggregateRelation[aggregateModel, int64]{})),
		a.Query().With(a.Count.Using(aggregateCount(r.With(child), sqlowner.Expressions()))),
		a.Query().With(a.Count.Using(aggregateCount(r.WithPivot(child), sqlowner.Expressions()))),
	} {
		if err := validateQuery(bad.state); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid aggregate accepted", err)
		}
	}
	derived := count.Using(aggregateCount(r.WherePivot(NumberEq(a.ParentID, 1)), sqlowner.Expressions()))
	if err := validateQuery(a.Query().With(derived).state); err != nil {
		t.Fatal(err)
	}
	if count.plan.slot != derived.plan.slot || len(count.plan.aggregate.input.plan.join.pivot.ast.Predicates) != 0 {
		t.Fatal("Using changed original slot or pivot scope")
	}
	cause := errors.New("construction failure")
	invalid := count
	invalid.plan = copyRelationPlan(count.plan)
	invalid.plan.err = cause
	if err := Check(count.Using(invalid)); !errors.Is(err, cause) {
		t.Fatal("Using lost construction failure", err)
	}
}
