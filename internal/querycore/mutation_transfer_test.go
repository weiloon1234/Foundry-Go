package querycore

import (
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func mutationTransferQuery() QueryCore {
	return QueryCore{Table: "users", Model: &ModelDefinition{Table: "users", Primary: "id", Columns: []Column{{Name: "id", DatabaseDefault: true}, {Name: "name"}, {Name: "enabled", DatabaseDefault: true}, {Name: "note", Nullable: true}}}}
}
func mutationTransferAssignment(table, name string, v driver.Value) MutationAssignment {
	return MutationAssignment{Field: FieldRef{Table: table, Column: name}, AssignmentValue: AssignmentValue{Bind: func() (driver.Value, error) { return v, nil }}}
}
func mutationTransferPredicate(column string, v driver.Value) Expression {
	return Comparison{Operand: FieldRef{Table: "users", Column: column}, Operator: Equal, Values: RetainComparisonValues([]any{v}), Bind: func(v any) (driver.Value, error) { return v, nil }}
}
func compileTransferredMutation(q QueryCore, kind MutationKind, assignments ...MutationAssignment) (Statement, error) {
	c, err := q.WriteCompiler(kind, true, false)
	if err != nil {
		return Statement{}, err
	}
	assigned, err := q.ValidateMutation(kind, assignments, &c)
	if err != nil {
		return Statement{}, err
	}
	node := q.ModelSelect(ColumnSelections(q.Table, q.Model.Columns))
	if kind == InsertModel {
		union := map[string]bool{}
		for name := range assigned {
			union[name] = true
		}
		return q.CompileInsert(c, []map[string]MutationAssignment{assigned}, union, node, nil)
	}
	return q.CompileMutation(c, node, kind, assignments, assigned, nil, false)
}
func TestMutationCompilerDefaultsNullsAndStableBindings(t *testing.T) {
	q := mutationTransferQuery()
	insert, err := compileTransferredMutation(q, InsertModel, mutationTransferAssignment("users", "name", "literal '; --"))
	want := `INSERT INTO "users" ("name") VALUES ($1) RETURNING "users"."id", "users"."name", "users"."enabled", "users"."note"`
	if err != nil || insert.SQLText != want || !reflect.DeepEqual(insert.Bindings, []any{"literal '; --"}) {
		t.Fatal(insert.SQLText, insert.Bindings, err)
	}
	q.Predicates = []Expression{mutationTransferPredicate("id", int64(42)), mutationTransferPredicate("name", "allowed")}
	update, err := compileTransferredMutation(q, UpdateModel, mutationTransferAssignment("users", "enabled", false), mutationTransferAssignment("users", "note", nil))
	if err != nil || !strings.Contains(update.SQLText, `SET "enabled" = $1, "note" = $2 WHERE ("users"."id" = $3) AND ("users"."name" = $4) RETURNING`) || !reflect.DeepEqual(update.Bindings, []any{false, nil, int64(42), "allowed"}) {
		t.Fatal(update.SQLText, update.Bindings, err)
	}
	remove, err := compileTransferredMutation(q, DeleteModel)
	if err != nil || !strings.HasPrefix(remove.SQLText, `DELETE FROM "users" WHERE ("users"."id" = $1)`) || !reflect.DeepEqual(remove.Bindings, []any{int64(42), "allowed"}) {
		t.Fatal(remove.SQLText, err)
	}
	defaults := QueryCore{Table: "users", Model: &ModelDefinition{Table: "users", Primary: "id", Columns: []Column{{Name: "id", DatabaseDefault: true}}}}
	s, err := compileTransferredMutation(defaults, InsertModel)
	if err != nil || !strings.Contains(s.SQLText, " DEFAULT VALUES RETURNING ") {
		t.Fatal(s.SQLText, err)
	}
}
func TestMutationCompilerRejectsUnsafeOrIncompleteWrites(t *testing.T) {
	valid := mutationTransferAssignment("users", "name", "valid")
	id := mutationTransferPredicate("id", int64(1))
	type plan struct {
		kind        MutationKind
		assignments []MutationAssignment
		predicates  []Expression
		limit       value.Optional[int]
		ordered     bool
	}
	for name, p := range map[string]plan{
		"missing required":   {kind: InsertModel},
		"unknown column":     {kind: InsertModel, assignments: []MutationAssignment{mutationTransferAssignment("users", "typo", "bad")}},
		"wrong table":        {kind: InsertModel, assignments: []MutationAssignment{mutationTransferAssignment("orders", "name", "bad")}},
		"duplicate field":    {kind: InsertModel, assignments: []MutationAssignment{valid, valid}},
		"null required":      {kind: InsertModel, assignments: []MutationAssignment{mutationTransferAssignment("users", "name", nil)}},
		"create filtered":    {kind: InsertModel, assignments: []MutationAssignment{valid}, predicates: []Expression{id}},
		"update unscoped":    {kind: UpdateModel, assignments: []MutationAssignment{valid}},
		"delete unscoped":    {kind: DeleteModel},
		"key in disjunction": {kind: DeleteModel, predicates: []Expression{Junction{Any: true, Children: []Expression{id, mutationTransferPredicate("name", "other")}}}},
		"negated key":        {kind: DeleteModel, predicates: []Expression{Negation{Child: id}}},
		"empty update":       {kind: UpdateModel, predicates: []Expression{id}},
		"primary mutation":   {kind: UpdateModel, predicates: []Expression{id}, assignments: []MutationAssignment{mutationTransferAssignment("users", "id", int64(2))}},
		"paged delete":       {kind: DeleteModel, predicates: []Expression{id}, limit: value.Set(1)},
		"ordered update":     {kind: UpdateModel, predicates: []Expression{id}, assignments: []MutationAssignment{valid}, ordered: true},
	} {
		t.Run(name, func(t *testing.T) {
			q := mutationTransferQuery()
			q.Predicates = p.predicates
			q.Limit = p.limit
			if p.ordered {
				q.Orders = []QueryOrder{{Field: FieldRef{Table: "users", Column: "id"}}}
			}
			if _, err := compileTransferredMutation(q, p.kind, p.assignments...); !errors.Is(err, fault.Invalid) && !errors.Is(err, fault.Missing) {
				t.Fatal("unsafe write compiled", err)
			}
		})
	}
}

func TestCompilerRejectsInvalidMetadataColumnsAndParameterBounds(t *testing.T) {
	node := structuralSelect("records")
	bind := func(v any) (driver.Value, error) { return v, nil }
	for name, change := range map[string]func(*SelectNode){
		"unknown predicate": func(n *SelectNode) {
			n.Predicates = []Expression{Comparison{Operand: FieldRef{Table: "records", Column: "missing"}, Operator: Equal, Values: RetainComparisonValues([]any{int64(1)}), Bind: bind}}
		},
		"unknown order": func(n *SelectNode) {
			n.Orders = []OrderNode{{Expression: FieldRef{Table: "records", Column: "missing"}}}
		},
		"parameter budget": func(n *SelectNode) {
			vs := make([]any, MaxParameters)
			for i := range vs {
				vs[i] = float64(i)
			}
			n.Predicates = []Expression{Comparison{Operand: FieldRef{Table: "records", Column: "id"}, Operator: In, Values: RetainComparisonValues(vs), Bind: bind}}
			n.Limit = value.Set(1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			n := node
			change(&n)
			if s, err := CompileRead(n, t.Context(), false, nil, true); !errors.Is(err, fault.Invalid) || s.SQLText != "" {
				t.Fatal("invalid compiler boundary accepted", err)
			}
		})
	}
	for _, d := range []ModelDefinition{
		{Table: "users", Primary: "missing", Columns: []Column{{Name: "id"}}},
		{Table: "users", Primary: "id", Columns: []Column{{Name: "id"}, {Name: "id"}}},
		{Table: strings.Repeat("a", 64), Primary: "id", Columns: []Column{{Name: "id"}}},
	} {
		if _, err := d.ValidateColumns(true); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid metadata accepted", err)
		}
	}
	d := ModelDefinition{Table: "users", Primary: "id", Columns: []Column{{Name: "id"}}}
	if _, err := d.ValidateColumns(false); !errors.Is(err, fault.Invalid) {
		t.Fatal("missing decoder accepted", err)
	}
}
