package rows

import (
	"errors"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

func rejectComputedSensitiveCursor[S, V any](t *testing.T, q Scalar[S, V]) {
	t.Helper()
	if _, err := CompileRead(ReadScalar(q)); err != nil {
		t.Fatal("valid sensitive expression rejected", err)
	}
	c := ValueCursorFor(Values(q))
	state := &driverState{}
	if _, err := c.UniqueBy(expr.Group(c.Value())).Paginate(t.Context(), openTest(t, state), orm.CursorRequest[V]{Size: 1}); !errors.Is(err, fault.Invalid) || !strings.Contains(err.Error(), "sensitive") || state.sql != "" {
		t.Fatal("sensitive expression reached cursor execution", err, state.sql)
	}
}

func TestComputedCursorSensitivityWithoutModelFields(t *testing.T) {
	m := declareMembers()
	q := From(m.Query())
	ordinary := expr.Value(m.Name)
	// The expression codec alone supplies sensitivity; model metadata stays public.
	core := expr.FoundryRowValue(ordinary, sqlowner.Expressions())
	col := expr.FoundrySelectedColumn(expr.Row(ordinary).Value(), sqlowner.Expressions())
	col.Sensitive = true
	core.Sensitive = true
	core.Node = querycore.WithSensitivity(core.Node, true)
	field := expr.FoundryRowExpression[member](core, col, sqlowner.Expressions())
	optional := expr.NullableRow(field)
	fallback := expr.Param(m.Name, "public")
	for name, expression := range map[string]expr.Selected[member, string]{
		"column comparison": expr.When(expr.Equal(field, ordinary), fallback).Else(expr.Param(m.Name, "other")).Value(),
		"membership":        expr.When(InQuery(field, SelectScalar(q, fallback.Value())), fallback).Else(expr.Param(m.Name, "other")).Value(),
		"lower":             expr.Lower(field).Value(),
		"nested operations": expr.Lower(expr.Concat(ordinary, field)).Value(),
		"coalesce":          expr.Coalesce(optional, fallback).Value(),
		"selected coalesce": expr.CoalesceValue(optional.Value(), fallback.Value()),
		"case result":       expr.When(ordinary.Eq("x"), fallback).Else(field).Value(),
		"selected case":     expr.WhenValue(expr.Grouped(ordinary.Eq("x")), fallback.Value()).Else(field.Value()),
		"case condition":    expr.When(field.Eq("x"), fallback).Else(expr.Param(m.Name, "other")).Value(),
	} {
		t.Run(name, func(t *testing.T) { rejectComputedSensitiveCursor(t, SelectScalar(q, expression)) })
	}
	for name, expression := range map[string]expr.Selected[member, value.Nullable[string]]{
		"nullable coalesce":          expr.CoalesceNullable(expr.NullableRow(fallback), optional).Value(),
		"selected nullable coalesce": expr.CoalesceNullableValue(expr.Nullable(fallback.Value()), optional.Value()),
		"nullif right":               expr.NullIf(ordinary, field).Value(),
		"selected nullif":            expr.NullIfValue(expr.Row(ordinary).Value(), field.Value()),
	} {
		t.Run(name, func(t *testing.T) { rejectComputedSensitiveCursor(t, SelectScalar(q, expression)) })
	}
	w := expr.WindowFor(ModelScope(m.Query()))
	rejectComputedSensitiveCursor(t, SelectScalar(q, expr.RowNumber(w.OrderBy(expr.OrderRow(field).Asc()))))
	rejectComputedSensitiveCursor(t, SelectScalar(q, expr.RowNumber(w.PartitionBy(expr.Group(field)))))
	lower := expr.Lower(field)
	rejectComputedSensitiveCursor(t, GroupByScalar(SelectScalar(q, lower.Value()), expr.Group(lower)))
	stored := core
	stored.Node = querycore.UnwrapSensitivity(stored.Node)
	rejectComputedSensitiveCursor(t, SelectScalar(q, expr.CountRow(expr.FoundryRowExpression[member](stored, col, sqlowner.Expressions())).Value()))
	numeric, numberColumn := expr.FoundryAggregate(expr.Sum(orm.NumberKey(m.ID)), sqlowner.Expressions())
	sum := numeric.Node.(querycore.AggregateNode)
	sum.Sensitive = true
	numeric.Node = sum
	rejectComputedSensitiveCursor(t, SelectScalar(q, expr.FoundrySelectedValue[member](numeric, numberColumn, sqlowner.Expressions())))
	safe := ValueCursorFor(Values(SelectScalar(q, expr.Lower(ordinary).Value())))
	safe = safe.UniqueBy(expr.Group(safe.Value()))
	if _, err := ormcore.PlanResultCursor(t.Context(), safe.core, 1, "", false, false); err != nil {
		t.Fatal("public expression rejected", err)
	}

}

type sensitivityRelationModel struct {
	ID       int64
	Rank     value.Nullable[int64]
	Children relation.Many[sensitivityRelationModel]
	Links    relation.Through[sensitivityRelationModel, sensitivityRelationModel]
}
type sensitivityRelationTable struct {
	orm.Table[sensitivityRelationModel, int64]
	Rank     orm.NullableNumber[sensitivityRelationModel, int64]
	Children orm.Many[sensitivityRelationModel, sensitivityRelationModel]
	Links    orm.Through[sensitivityRelationModel, sensitivityRelationModel, sensitivityRelationModel]
}

func TestRelatedAggregateConditionalCursorSensitivity(t *testing.T) {
	for _, metadata := range []bool{false, true} {
		columnCodec := codec.Signed[int64]()
		if metadata {
			columnCodec = columnCodec.WithSensitiveValues()
		}
		d := orm.Define("sensitivity_relations", func(b orm.Builder[sensitivityRelationModel], m *sensitivityRelationModel) sensitivityRelationTable {
			return sensitivityRelationTable{orm.TableOf(b, &m.ID), orm.NullableNumberOf(b, &m.Rank, orm.Codec(columnCodec)), orm.ManyOf(b, &m.Children), orm.ThroughOf(b, &m.Links)}
		})
		direct := orm.HasManyOf(d.Children, d.Key(), d.Key())
		through := orm.ManyToManyOf(d.Links, d.Key(), d.Key(), d.Key(), d.Key())
		measure := expr.Min(d.Rank)
		core, col := expr.FoundryAggregate(measure, sqlowner.Expressions())
		// Vary the two independent provenance sources: declaration metadata or
		// the aggregate's input codec. The result codec itself remains public.
		node := core.Node.(querycore.AggregateNode)
		node.Sensitive = !metadata
		core.Node = node
		core.Sensitive = false
		col.Sensitive = false
		for name, source := range map[string]orm.AggregateSource[sensitivityRelationModel, sensitivityRelationModel]{"direct": direct, "through": through, "pivot": orm.Pivot(through)} {
			t.Run(fmt.Sprintf("%s/metadata=%v", name, metadata), func(t *testing.T) {
				related := orm.FoundryRelatedValue(source, core, col, sqlowner.Expressions())
				minimum := expr.FoundryRowExpression[sensitivityRelationModel](related, col, sqlowner.Expressions())
				selected := expr.Coalesce(minimum, expr.Param(d.Key(), int64(0)))
				rejectComputedSensitiveCursor(t, SelectScalar(From(d.Query()), selected.Value()))
			})
		}
		safe := expr.Coalesce(expr.RelatedValue(direct, expr.Min(orm.NumberKey(d.Key()))), expr.Param(d.Key(), int64(0)))
		cursor := ValueCursorFor(Values(SelectScalar(From(d.Query()), safe.Value())))
		cursor = cursor.UniqueBy(expr.Group(cursor.Value()))
		if _, err := ormcore.PlanResultCursor(t.Context(), cursor.core, 1, "", false, false); err != nil {
			t.Fatal("public relation output rejected", err)
		}
	}
}
