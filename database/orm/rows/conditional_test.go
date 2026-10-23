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
	q := expressionQuery(a, first.Value(), codec.Signed[int64]()).Where(expr.Coalesce(expr.NullFor(id), id).Eq(5)).OrderBy(first.Asc())
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
	got, err := q.All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 1 || got[0].Value != 10 {
		t.Fatal(got, err)
	}
}

func TestConditionalNullableResultsAndComposition(t *testing.T) {
	m := declareMembers()
	a := As[left](m.Query(), "m")
	name := expr.At(Scope(a), m.Name)
	nickname := expr.At(Scope(a), m.Nickname)
	fallback := expr.Select(name).Param("")
	required := expr.Coalesce(nickname, fallback)
	for _, selected := range []expr.Selected[Alias[left, member], string]{required.Value(), expr.CoalesceValue(expr.Select(nickname), fallback.Value())} {
		state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{""}, {"Ada"}}}
		got, err := expressionQuery(a, selected, codec.String[string]()).All(t.Context(), openTest(t, state))
		if err != nil || len(got) != 2 || got[0].Value != "" || got[1].Value != "Ada" {
			t.Fatal(got, err)
		}
	}
	for name, selected := range map[string]expr.Selected[Alias[left, member], value.Nullable[string]]{
		"coalesce_row":             expr.CoalesceNullable(nickname, expr.NullFor(name)).Value(),
		"coalesce_selected":        expr.CoalesceNullableValue(expr.Select(nickname), expr.NullFor(name).Value()),
		"nullif_row":               expr.NullIf(required, fallback).Value(),
		"nullif_selected":          expr.NullIfValue(required.Value(), fallback.Value()),
		"nullif_nullable_row":      expr.NullIfNullable(nickname, expr.NullFor(name)).Value(),
		"nullif_nullable_selected": expr.NullIfNullableValue(expr.Select(nickname), expr.NullFor(name).Value()),
		"case_null":                expr.When(nickname.IsNull(), fallback).ElseNull().Value(),
		"case_nullable":            expr.When(nickname.IsNull(), expr.NullableRow(fallback)).Else(nickname).Value(),
	} {
		t.Run(name, func(t *testing.T) {
			state := &driverState{columns: []string{"value"}, rows: [][]driver.Value{{nil}, {"Ada"}}}
			got, err := expressionQuery(a, selected, codec.Nullable(codec.String[string]())).All(t.Context(), openTest(t, state))
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
	got, err := q.All(t.Context(), openTest(t, state))
	if err != nil || len(got) != 1 || got[0].Value != 0 {
		t.Fatal(got, err)
	}
	expressionSQL(t, expressionQuery(a, nullable, codec.Nullable(codec.Signed[int64]())))
	// Explicitly lifting a row condition does not make its column grouped.
	invalid := expr.WhenValue(expr.Grouped(expr.OrderRow(expr.At(Scope(a), o.MemberID)).Gt(0)), count.Value()).Else(count.Param(0).Value())
	state = &driverState{}
	result, err := expressionQuery(a, invalid, codec.Signed[int64]()).All(t.Context(), openTest(t, state))
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
		"coalesce_fallback":  expr.Coalesce(expr.NullFor(own), foreign).Value(),
		"param":              expr.When(own.Eq(1), p).Else(foreign).Param(2).Value(),
		"zero_case":          (expr.Case[Alias[right, order], int64]{}).Else(p).Value(),
		"zero_selected_case": (expr.ValueCase[Alias[right, order], int64]{}).Else(p.Value()),
		"nil_coalesce":       expr.Coalesce[Alias[right, order], int64](nil, p).Value(),
	} {
		t.Run(name, func(t *testing.T) {
			state := &driverState{}
			got, err := expressionQuery(a, selected, codec.Signed[int64]()).All(t.Context(), openTest(t, state))
			if !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
				t.Fatal("conditional lost invalid operand", got, err, state.sql)
			}
		})
	}
	nested := expr.When(expr.OrderRow(own).Gt(0), expr.NullableRow(p)).ElseNull()
	state := &driverState{}
	got, err := expressionQuery(a, nested.Value(), codec.Nullable(codec.Nullable(codec.Signed[int64]()))).All(t.Context(), openTest(t, state))
	if !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
		t.Fatal("CASE nested NULL wrappers", got, err, state.sql)
	}
	invalidNullif := expr.NullIf(expr.NullableRow(p), expr.NullableRow(p))
	state = &driverState{}
	got, err = expressionQuery(a, invalidNullif.Value(), codec.Nullable(codec.Nullable(codec.Signed[int64]()))).All(t.Context(), openTest(t, state))
	if !errors.Is(err, fault.Invalid) || got != nil || state.sql != "" {
		t.Fatal("NULLIF nested NULL wrappers", got, err, state.sql)
	}
}
