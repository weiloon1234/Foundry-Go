package rows

import (
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestConditionalBindingsAndImmutableCase(t *testing.T) {
	o := declareOrders()
	a := As[right](o.Query(), "o")
	id := expr.At(Scope(a), o.MemberID)
	parameter := expr.OrderRow(id).Param
	base := expr.When(id.Eq(1), parameter(10))
	first := base.Else(parameter(20))
	derived := base.When(id.Eq(2), parameter(30)).Else(parameter(40))
	q := OrderBy(expressionQuery(a, first.Value(), codec.Signed[int64]()).Where(expr.Coalesce(expr.NullableRow(id).Param(value.Null[int64]()), id).Eq(5)), first.Asc())
	s := expressionSQL(t, q)
	if !strings.Contains(s.SQLText, `CASE WHEN ("o"."member_id" = $1) THEN CAST($2 AS bigint) ELSE CAST($3 AS bigint) END`) || !reflect.DeepEqual(s.Bindings, []any{int64(1), int64(10), int64(20), nil, int64(5), int64(1), int64(10), int64(20)}) {
		t.Fatal(s.SQLText, s.Bindings)
	}
	for name, choice := range map[string]expr.RowExpression[Alias[right, order], int64]{"base": first, "derived": derived, "base_again": base.Else(parameter(50))} {
		core := expr.FoundrySelected(choice.Value(), sqlowner.Expressions())
		branches := len(core.Node.(querycore.CaseNode).Branches)
		want := 1
		if name == "derived" {
			want = 2
		}
		if branches != want || len(core.Refs) != 2 {
			t.Fatal("CASE derivation changed", name, branches, core.Refs)
		}
	}
	state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{int64(10)}}}
	got, err := Read(q).All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 1 || got[0].Value != 10 {
		t.Fatal(got, err)
	}
}

func TestConditionalNullableResultsAndComposition(t *testing.T) {
	m := declareMembers()
	a := As[left](m.Query(), "m")
	name := expr.At(Scope(a), m.Name)
	nickname := expr.At(Scope(a), m.Nickname)
	fallback := expr.Row(name).Value().Param("")
	required := expr.Coalesce(nickname, fallback)
	for _, selected := range []expr.Selected[Alias[left, member], string]{required.Value(), expr.CoalesceValue(expr.Row(nickname).Value(), fallback.Value())} {
		state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{""}, {"Ada"}}}
		got, err := Read(expressionQuery(a, selected, codec.String[string]())).All(t.Context(), openTest(t, state))
		if err != nil || len(got) != 2 || got[0].Value != "" || got[1].Value != "Ada" {
			t.Fatal(got, err)
		}
	}
	for name, selected := range map[string]expr.Selected[Alias[left, member], value.Nullable[string]]{
		"coalesce_row":             expr.CoalesceNullable(nickname, expr.NullableRow(name).Param(value.Null[string]())).Value(),
		"coalesce_selected":        expr.CoalesceNullableValue(expr.Row(nickname).Value(), expr.NullableRow(name).Param(value.Null[string]()).Value()),
		"nullif_row":               expr.NullIf(required, fallback).Value(),
		"nullif_selected":          expr.NullIfValue(required.Value(), fallback.Value()),
		"nullif_nullable_row":      expr.NullIfNullable(nickname, expr.NullableRow(name).Param(value.Null[string]())).Value(),
		"nullif_nullable_selected": expr.NullIfNullableValue(expr.Row(nickname).Value(), expr.NullableRow(name).Param(value.Null[string]()).Value()),
		"case_null":                expr.When(nickname.IsNull(), fallback).ElseNull().Value(),
		"case_nullable":            expr.When(nickname.IsNull(), expr.NullableRow(fallback)).Else(nickname).Value(),
	} {
		t.Run(name, func(t *testing.T) {
			state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{nil}, {"Ada"}}}
			got, err := Read(expressionQuery(a, selected, codec.Nullable(codec.String[string]()))).All(t.Context(), openTest(t, state))
			if err != nil || len(got) != 2 || !got[0].Value.IsNull() {
				t.Fatal(got, err)
			}
			if s, ok := got[1].Value.Get(); !ok || s != "Ada" {
				t.Fatal(got)
			}
		})
	}
}

func TestSelectedCaseAndGroupConditions(t *testing.T) {
	o := declareOrders()
	a := As[right](o.Query(), "o")
	count := expr.Count[Alias[right, order]]()
	selected := expr.WhenValue(count.Gt(0), count.Value()).When(count.Gt(10), count.Param(10).Value()).Else(count.Param(0).Value())
	s := expressionSQL(t, expressionQuery(a, selected, codec.Signed[int64]()))
	if !strings.Contains(s.SQLText, "CASE WHEN (COUNT(*) > $1)") || !reflect.DeepEqual(s.Bindings, []any{int64(0), int64(10), int64(10), int64(0)}) {
		t.Fatal(s.SQLText, s.Bindings)
	}
	nullable := expr.WhenValue(count.Gt(0), count.Value()).ElseNull()
	q := expressionQuery(a, expr.CoalesceValue(expr.NullIfValue(selected, count.Param(0).Value()), count.Param(0).Value()), codec.Signed[int64]())
	state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{int64(0)}}}
	got, err := Read(q).All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 1 || got[0].Value != 0 {
		t.Fatal(got, err)
	}
	expressionSQL(t, expressionQuery(a, nullable, codec.Nullable(codec.Signed[int64]())))
	// Explicitly lifting a row condition does not make its column grouped.
	invalid := expr.WhenValue(expr.Grouped(expr.OrderRow(expr.At(Scope(a), o.MemberID)).Gt(0)), count.Value()).Else(count.Param(0).Value())
	state = &driverState{}
	result, err := Read(expressionQuery(a, invalid, codec.Signed[int64]())).All(t.Context(), openTest(t, state))
	if !errors.Is(err, fault.Invalid) || result != nil || state.sql != "" {
		t.Fatal("ungrouped CASE accepted", result, err, state.sql)
	}
}

func TestConditionalsPreserveAllOwnersAndErrors(t *testing.T) {
	o, other := declareOrders(), declareOrders()
	a := As[right](o.Query(), "o")
	own, foreign := expr.At(Scope(a), o.MemberID), expr.At(Scope(a), other.MemberID)
	p := expr.OrderRow(own).Param(1)
	for name, selected := range map[string]expr.Selected[Alias[right, order], int64]{
		"condition":          expr.When(foreign.Eq(1), p).Else(p).Value(),
		"result":             expr.When(own.Eq(1), foreign).Else(p).Value(),
		"fallback":           expr.When(own.Eq(1), p).Else(foreign).Value(),
		"coalesce_first":     expr.Coalesce(expr.NullableRow(foreign), p).Value(),
		"coalesce_fallback":  expr.Coalesce(expr.NullableRow(own).Param(value.Null[int64]()), foreign).Value(),
		"param":              expr.When(own.Eq(1), p).Else(foreign).Param(2).Value(),
		"zero_case":          (expr.Case[Alias[right, order], int64]{}).Else(p).Value(),
		"zero_selected_case": (expr.ValueCase[Alias[right, order], int64]{}).Else(p.Value()),
		"nil_coalesce":       expr.Coalesce[Alias[right, order], int64](nil, p).Value(),
	} {
		t.Run(name, func(t *testing.T) {
			state := &driverState{}
			got, err := Read(expressionQuery(a, selected, codec.Signed[int64]())).All(t.Context(), openTest(t, state))
			if !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
				t.Fatal("conditional lost invalid operand", got, err, state.sql)
			}
		})
	}
	nested := expr.When(expr.OrderRow(own).Gt(0), expr.NullableRow(p)).ElseNull()
	state := &driverState{}
	got, err := Read(expressionQuery(a, nested.Value(), codec.Nullable(codec.Nullable(codec.Signed[int64]())))).All(t.Context(), openTest(t, state))
	if !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
		t.Fatal("CASE nested NULL wrappers", got, err, state.sql)
	}
	invalidNullif := expr.NullIf(expr.NullableRow(p), expr.NullableRow(p))
	state = &driverState{}
	got, err = Read(expressionQuery(a, invalidNullif.Value(), codec.Nullable(codec.Nullable(codec.Signed[int64]())))).All(t.Context(), openTest(t, state))
	if !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
		t.Fatal("NULLIF nested NULL wrappers", got, err, state.sql)
	}
}

func TestEmptyMembershipRejectsInvalidOperand(t *testing.T) {
	m := declareMembers()
	invalid := expr.Param(m.Name, "\x00")
	q := WhereScalar(SelectScalar(From(m.Query()), expr.Row(expr.Value(m.ID)).Value()), invalid.In())
	if _, err := CompileRead(ReadScalar(q)); !errors.Is(err, fault.Invalid) {
		t.Fatal("empty IN concealed invalid operand", err)
	}
	state := &driverState{}
	if got, err := ReadScalar(q).All(t.Context(), openTest(t, state)); !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
		t.Fatal("empty IN reached executor", got, err, state.sql)
	}
}

func TestParameterCaptureAndCodecTypes(t *testing.T) {
	encodings := 0
	c := codec.New(func(v *[2]byte) (driver.Value, error) { encodings++; return v[:], nil }, func(any) (*[2]byte, error) { return nil, nil }).WithParameterType(codec.TypeBytes)
	original := &[2]byte{1, 2}
	p := expr.Constant[member](c, original)
	original[0] = 9
	q := SelectScalar(From(declareMembers().Query()), p.Value())
	first, err := CompileRead(ReadScalar(q))
	if err != nil {
		t.Fatal(err)
	}
	first.Arguments()[0].([]byte)[0] = 8
	raw, err := querycore.CompileRead(q.source.Node, t.Context(), false, q.source.Err, true)
	if err != nil {
		t.Fatal(err)
	}
	raw.Bindings[0].([]byte)[0] = 7
	second, err := CompileRead(ReadScalar(q))
	if err != nil || encodings != 1 || !reflect.DeepEqual(second.Arguments(), []any{[]byte{1, 2}}) {
		t.Fatal("parameter capture ownership", second.Arguments(), encodings, err)
	}
	m := declareMembers()
	for _, c := range []codec.Codec[string]{codec.New(func(v string) (driver.Value, error) { return v, nil }, func(any) (string, error) { return "", nil }), codec.String[string]().WithParameterType(255), (codec.Codec[string]{}).WithParameterType(codec.TypeText)} {
		if _, err := CompileRead(ReadScalar(SelectScalar(From(m.Query()), expr.Constant[member](c, "x").Value()))); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid parameter metadata", err)
		}
	}
	if _, err := CompileRead(ReadScalar(SelectScalar(From(m.Query()), expr.Param(m.Name, "\x00").Value()))); !errors.Is(err, fault.Invalid) {
		t.Fatal("invalid parameter value", err)
	}
	if got := codec.Nullable(codec.Signed[int64]().Validated(func(int64) error { return nil })).ParameterType(); got != codec.TypeInteger {
		t.Fatal("adapter lost parameter type", got)
	}
	qbad := WhereScalar(SelectScalar(From(m.Query()), expr.Row(expr.Value(m.ID)).Value()), expr.NullableRow(expr.Value(m.ID)).Param(value.Null[int64]()).Eq(value.Null[int64]()))
	if _, err := CompileRead(ReadScalar(qbad)); !errors.Is(err, fault.Invalid) {
		t.Fatal("literal NULL equality accepted", err)
	}
}
func TestZeroCaseAndDistinctParameterIdentity(t *testing.T) {
	m := declareMembers()
	id := expr.Value(m.ID)
	q := From(m.Query())
	if _, err := CompileRead(ReadScalar(SelectScalar(q, (expr.Case[member, int64]{}).When(id.Eq(1), id).ElseNull().Value()))); err != nil {
		t.Fatal("zero CASE failed to acquire branch", err)
	}
	first := expr.When(id.Eq(1), expr.Param(m.ID, int64(10))).Else(expr.Param(m.ID, int64(20)))
	s, err := CompileRead(ReadScalar(OrderScalar(DistinctScalar(SelectScalar(q, first.Value())), expr.OrderRow(first).Asc())))
	if err != nil || !strings.Contains(s.SQL(), "ORDER BY 1 ASC") || len(s.Arguments()) != 3 {
		t.Fatal("DISTINCT CASE rebound parameters", s.SQL(), s.Arguments(), err)
	}
}
func TestConditionalValuePhaseAndBounds(t *testing.T) {
	d := declareMembers()
	source := From(d.Query())
	id := expr.Value(d.ID)
	count := expr.Count[member]()
	grouped := expr.WhenValue(expr.Grouped(id.Eq(1)), count.Value()).Else(count.Param(0).Value())
	if _, err := CompileRead(ReadScalar(GroupByScalar(SelectScalar(source, grouped), expr.Group(id)))); err != nil {
		t.Fatal("grouped condition rejected", err)
	}
	window := expr.RowNumber(expr.WindowFor(ModelScope(d.Query())).OrderBy(id.Asc()))
	wrapped := expr.WhenValue(expr.Grouped(expr.OrderRow(id).Gt(0)), window).Else(window.Param(0).Value())
	if _, err := CompileRead(ReadScalar(SelectScalar(source, wrapped))); err != nil {
		t.Fatal(err)
	}
	if _, err := CompileRead(ReadScalar(SelectScalar(source, expr.Lag(wrapped, 1, expr.WindowFor(ModelScope(d.Query())))))); !errors.Is(err, fault.Invalid) {
		t.Fatal("CASE concealed nested window", err)
	}
	wrappedNode := expr.FoundrySelected(wrapped, sqlowner.Expressions()).Node
	node := source.source.Node
	node.Selections = []querycore.SelectItem{{Expression: querycore.AggregateNode{Kind: querycore.CountAll}}}
	comparison := func(v querycore.ValueExpression) querycore.Expression {
		return querycore.BinaryComparison{Left: v, Right: querycore.ParameterNode{Kind: codec.TypeInteger, Value: querycore.RetainLiteral(int64(1))}, Operator: querycore.Equal}
	}
	node.Having = []querycore.Expression{comparison(wrappedNode)}
	if _, err := querycore.CompileRead(node, t.Context(), false, nil, true); !errors.Is(err, fault.Invalid) {
		t.Fatal("CASE concealed window in HAVING", err)
	}
	cyclic := querycore.ConditionalNode{Kind: querycore.CoalesceValues, Arguments: make([]querycore.ValueExpression, 1)}
	cyclic.Arguments[0] = cyclic
	for name, v := range map[string]querycore.ValueExpression{
		"empty CASE": querycore.CaseNode{}, "empty coalesce": querycore.ConditionalNode{Kind: querycore.CoalesceValues}, "invalid NULLIF": querycore.ConditionalNode{Kind: querycore.NullIfValues, Arguments: []querycore.ValueExpression{querycore.FieldRef{Table: "members", Column: "id"}}},
		"nested aggregate": querycore.ConditionalNode{Kind: querycore.CoalesceValues, Arguments: []querycore.ValueExpression{querycore.AggregateNode{Kind: querycore.CountAll}}},
		"nested window":    querycore.ConditionalNode{Kind: querycore.CoalesceValues, Arguments: []querycore.ValueExpression{querycore.WindowNode{Kind: querycore.RowNumberWindow}}}, "cycle": cyclic,
		"bound": querycore.ConditionalNode{Kind: querycore.CoalesceValues, Arguments: make([]querycore.ValueExpression, querycore.MaxExpressionNodes+1)},
	} {
		t.Run(name, func(t *testing.T) {
			n := source.source.Node
			n.Selections = []querycore.SelectItem{{Expression: querycore.FieldRef{Table: "members", Column: "id"}}}
			n.Predicates = []querycore.Expression{comparison(v)}
			if _, err := querycore.CompileRead(n, t.Context(), false, nil, true); !errors.Is(err, fault.Invalid) {
				t.Fatal("invalid row conditional", err)
			}
			if name == "cycle" || name == "bound" {
				n.Predicates = nil
				n.Selections = []querycore.SelectItem{{Expression: v}}
				if _, err := querycore.CompileRead(n, t.Context(), false, nil, true); !errors.Is(err, fault.Invalid) {
					t.Fatal("invalid selected conditional", err)
				}
			}
		})
	}
}

func TestConditionalRejectsNestedNullability(t *testing.T) {
	m := declareMembers()
	id := expr.Value(m.ID)
	for _, v := range []expr.Selected[member, value.Nullable[value.Nullable[int64]]]{expr.When(id.Eq(1), expr.NullableRow(id)).ElseNull().Value(), expr.NullIf(expr.NullableRow(id), expr.NullableRow(id)).Value()} {
		state := &driverState{}
		if _, err := ReadScalar(SelectScalar(From(m.Query().Limit(0)), v)).All(t.Context(), openTest(t, state)); !errors.Is(err, fault.Invalid) || state.sql != "" {
			t.Fatal("nested NULL wrapper accepted", err, state.sql)
		}
	}
}
