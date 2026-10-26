package querycore

import (
	"database/sql/driver"
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
	"reflect"
	"strings"
	"testing"
)

func conflictTransferQuery() QueryCore {
	return QueryCore{Table: "counters", Model: &ModelDefinition{Table: "counters", Primary: "id", Columns: []Column{{Name: "id", DatabaseDefault: true}, {Name: "name"}, {Name: "count", DatabaseDefault: true}, {Name: "note", Nullable: true}}}}
}
func conflictField(table, col string) FieldRef { return FieldRef{Table: table, Column: col} }
func conflictCmp(f FieldRef, op Operator, v any) Expression {
	return Comparison{Operand: f, Operator: op, Values: RetainComparisonValues([]any{v}), Bind: func(x any) (driver.Value, error) { return x, nil }}
}
func compileConflictTransfer(p Conflict, empty bool) (Statement, error) {
	q := conflictTransferQuery()
	c, e := q.WriteCompiler(InsertModel, false, false)
	if e != nil {
		return Statement{}, e
	}
	if e = p.ValidateShape(q, &c, func(ConflictUpdate) error { return nil }); e != nil {
		return Statement{}, e
	}
	if e = p.ValidateRows(&c); e != nil {
		return Statement{}, e
	}
	check := q
	check.Predicates = p.Condition
	if e = check.Validate(); e != nil {
		return Statement{}, e
	}
	node := p.Dependencies(q.ModelSelect(ColumnSelections(q.Table, q.Model.Columns)))
	a := mutationTransferAssignment("counters", "name", "counter")
	rows := []map[string]MutationAssignment{{"name": a}}
	if empty {
		rows = nil
	}
	return q.CompileInsert(c, rows, map[string]bool{"name": true}, node, &p)
}
func conflictBase() Conflict {
	return Conflict{Keys: []ValueExpression{conflictField("counters", "name")}, Action: ConflictDoUpdate, Updates: []ConflictUpdate{{Field: conflictField("counters", "count"), Incoming: true}}}
}
func conflictAdd(a, b ValueExpression) OperationNode {
	return OperationNode{Kind: AddOperation, Result: codec.TypeInteger, Arguments: []OperationArgument{{Value: a, Kind: codec.TypeInteger}, {Value: b, Kind: codec.TypeInteger}}}
}
func TestConflictRowCalculationsAndConditions(t *testing.T) {
	stored, proposed := conflictField(ConflictStoredTable, "count"), conflictField(ConflictProposedTable, "count")
	p := conflictBase()
	p.Updates = []ConflictUpdate{{Field: conflictField("counters", "count"), Value: conflictAdd(stored, conflictAdd(proposed, ParameterNode{Kind: codec.TypeInteger, Value: RetainLiteral(int64(2))}))}, {Field: conflictField("counters", "note"), Value: conflictField(ConflictProposedTable, "note")}}
	p.Condition = []Expression{conflictCmp(conflictField("counters", "name"), NotEqual, "blocked")}
	p.RowCondition = []Expression{BinaryComparison{Left: proposed, Right: stored, Operator: Greater}}
	s, e := compileConflictTransfer(p, false)
	for _, want := range []string{`"count" = (CAST("foundry_upsert"."count" AS bigint) +`, `CAST("excluded"."count" AS bigint) +`, `"note" = "excluded"."note"`, `WHERE ("foundry_upsert"."name" <> $3) AND ("excluded"."count" > "foundry_upsert"."count")`} {
		if e != nil || !strings.Contains(s.SQLText, want) {
			t.Fatal(want, s.SQLText, e)
		}
	}
	if !reflect.DeepEqual(s.Bindings, []any{"counter", int64(2), "blocked"}) {
		t.Fatal(s.Bindings)
	}
	again, e := compileConflictTransfer(p, false)
	if e != nil || !reflect.DeepEqual(s, again) {
		t.Fatal("recompile mutated", e)
	}
}
func TestConflictIndexExpressionsAndSchemaConstants(t *testing.T) {
	name := conflictField("counters", "name")
	lower := OperationNode{Kind: LowerOperation, Result: codec.TypeText, Arguments: []OperationArgument{{Value: name, Kind: codec.TypeText}}}
	p := conflictBase()
	p.Keys = []ValueExpression{lower, conflictField("counters", "count")}
	p.TargetCondition = []Expression{Comparison{Operand: conflictField("counters", "note"), Operator: IsNull}, conflictCmp(name, NotEqual, "O'Reilly\\path")}
	a := mutationTransferAssignment("counters", "name", "new-bound-value")
	p.Updates = []ConflictUpdate{{Field: name, Literal: &a.AssignmentValue}}
	p.Condition = []Expression{conflictCmp(conflictField("counters", "count"), Greater, int64(1))}
	s, e := compileConflictTransfer(p, false)
	for _, want := range []string{`ON CONFLICT ((LOWER(CAST("name" AS text))), "count") WHERE ("note" IS NULL)`, `("name" <> E'O''Reilly\\path') DO UPDATE SET "name" = $2`, `WHERE ("foundry_upsert"."count" > $3)`} {
		if e != nil || !strings.Contains(s.SQLText, want) {
			t.Fatal(want, s.SQLText, e)
		}
	}
	if strings.Contains(s.SQLText, "new-bound-value") || !reflect.DeepEqual(s.Bindings, []any{"counter", "new-bound-value", int64(1)}) {
		t.Fatal(s.SQLText, s.Bindings)
	}
	again, e := compileConflictTransfer(p, false)
	if e != nil || !reflect.DeepEqual(s, again) {
		t.Fatal("index policy changed", e)
	}
}
func TestConflictRowsAndIndexesRejectInvalidExpressions(t *testing.T) {
	name := conflictField("counters", "name")
	lower := OperationNode{Kind: LowerOperation, Result: codec.TypeText, Arguments: []OperationArgument{{Value: name, Kind: codec.TypeText}}}
	sub := Subquery{Node: structuralSelect("lookup")}
	var invalid []Conflict
	for _, keys := range [][]ValueExpression{{nil}, {name, name}, {lower, lower}, {conflictField("other", "name")}, {ScalarSubquery{Query: sub}}, {CaseNode{Branches: []CaseBranch{{Condition: SubqueryPredicate{Query: sub}, Result: name}}, Otherwise: name}}, {AggregateNode{}}, {WindowNode{}}, make([]ValueExpression, MaxExpressionNodes+1)} {
		p := conflictBase()
		p.Keys = keys
		invalid = append(invalid, p)
	}
	for _, cond := range []Expression{nil, conflictCmp(conflictField("other", "name"), Equal, "x"), SubqueryPredicate{Query: sub}} {
		p := conflictBase()
		p.TargetCondition = []Expression{cond}
		invalid = append(invalid, p)
	}
	for _, named := range []bool{true, false} {
		p := conflictBase()
		p.Keys = nil
		p.Named = named
		p.Constraint = "counter_name_key"
		p.TargetCondition = []Expression{conflictCmp(name, Equal, "schema")}
		invalid = append(invalid, p)
	}
	for _, v := range []ValueExpression{nil, conflictField("other", "count"), conflictField(ConflictProposedTable, "typo"), AggregateNode{}, WindowNode{}} {
		p := conflictBase()
		p.Updates[0].Incoming = false
		p.Updates[0].Value = v
		invalid = append(invalid, p)
	}
	p := conflictBase()
	p.Updates[0].Value = AggregateNode{}
	invalid = append(invalid, p)
	p = conflictBase()
	p.RowCondition = []Expression{nil}
	invalid = append(invalid, p)
	p = conflictBase()
	p.Action = ConflictNothing
	p.Updates = nil
	p.RowCondition = []Expression{conflictCmp(conflictField(ConflictStoredTable, "count"), Equal, int64(0))}
	invalid = append(invalid, p)
	p = conflictBase()
	p.Updates[0].Field = FieldRef{}
	invalid = append(invalid, p)
	var v ValueExpression = conflictField(ConflictProposedTable, "count")
	for range MaxExpressionDepth + 1 {
		v = conflictAdd(v, conflictField(ConflictProposedTable, "count"))
	}
	p = conflictBase()
	p.Updates[0].Incoming = false
	p.Updates[0].Value = v
	invalid = append(invalid, p)
	for i, p := range invalid {
		for _, empty := range []bool{false, true} {
			if _, e := compileConflictTransfer(p, empty); !errors.Is(e, fault.Invalid) {
				t.Fatal("invalid conflict", i, empty, e)
			}
		}
	}
}
func TestConflictFilterAndAssignmentDiscoverCTEs(t *testing.T) {
	q := conflictTransferQuery()
	node := q.ModelSelect(ColumnSelections("counters", q.Model.Columns))
	node.Predicates = []Expression{conflictCmp(conflictField("counters", "name"), Equal, "lookup")}
	cte := CteNode{Name: "foundry_upsert", Query: node, Columns: q.Model.Columns}
	inner := SelectNode{Source: TableSource{Cte: &cte, Alias: "lookup", Columns: cte.Columns}, Selections: []SelectItem{{Expression: conflictField("lookup", "name")}}}
	p := conflictBase()
	p.Condition = []Expression{SubqueryPredicate{Operand: conflictField("counters", "name"), Query: Subquery{Node: inner}}}
	s, e := compileConflictTransfer(p, false)
	if e != nil || !strings.HasPrefix(s.SQLText, `WITH "foundry_upsert"`) || !strings.Contains(s.SQLText, `INSERT INTO "counters" AS "foundry_upsert_2"`) || !reflect.DeepEqual(s.Bindings, []any{"lookup", "counter"}) {
		t.Fatal(s.SQLText, s.Bindings, e)
	}
	inner.Selections = []SelectItem{{Expression: conflictField("lookup", "count")}}
	inner.Predicates = []Expression{BinaryComparison{Left: conflictField("lookup", "count"), Right: conflictField(ConflictStoredTable, "count"), Operator: Greater}}
	inner.Limit = value.Set(1)
	p = conflictBase()
	p.Updates[0].Incoming = false
	p.Updates[0].Value = ConditionalNode{Kind: CoalesceValues, Arguments: []ValueExpression{ScalarSubquery{Query: Subquery{Node: inner, Correlation: &ScopeRequirement{Sources: map[string][]Column{ConflictStoredTable: q.Model.Columns}}}}, conflictField(ConflictProposedTable, "count")}}
	s, e = compileConflictTransfer(p, false)
	if e != nil || !strings.HasPrefix(s.SQLText, `WITH "foundry_upsert"`) || !strings.Contains(s.SQLText, `INSERT INTO "counters" AS "foundry_upsert_2"`) || !strings.Contains(s.SQLText, `"lookup"."count" > "foundry_upsert_2"."count"`) || !reflect.DeepEqual(s.Bindings, []any{"lookup", "counter", int64(1)}) {
		t.Fatal(s.SQLText, s.Bindings, e)
	}
}
