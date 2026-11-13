package rows

import (
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestProjectionRebindingRetainsSQLCollationCapability(t *testing.T) {
	type status string
	type model struct {
		ID       int64
		Status   status
		Optional value.Nullable[status]
	}
	type table struct {
		orm.Table[model, int64]
		Status   orm.Text[model, status]
		Optional orm.NullableText[model, status]
	}
	type output struct {
		Status   status
		Optional value.Nullable[status]
	}
	type fields struct {
		Row[output]
		Status   Field[output, status]
		Optional Field[output, value.Nullable[status]]
	}
	schema := Define(func(b Builder[output], r *output) fields {
		return fields{b.Row(), TextOf(b, &r.Status), NullableTextOf(b, &r.Optional)}
	})
	project := func(c codec.Codec[status]) Query[model, output] {
		m := orm.Define("statuses", func(b orm.Builder[model], m *model) table {
			return table{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Status, orm.Codec(c)), orm.NullableTextOf(b, &m.Optional, orm.Codec(c))}
		})
		// Map and MapSelected must both retain the source SQL capability.
		return Select(From(m.Query()), schema,
			Map(schema.Status, expr.Value(orm.TextExpr(m.Status))),
			MapSelected(schema.Optional, expr.AsSelected(expr.Row(expr.Value(orm.NullableTextExpr(m.Optional))))))
	}
	text := project(codec.String[status]())
	native := project(codec.String[status]().WithoutCollation())
	for _, tc := range []struct {
		name       string
		query      Query[model, output]
		collatable bool
	}{{"native_enum", native, false}, {"text_shared_declaration", text, true}} {
		t.Run(tc.name, func(t *testing.T) {
			record := RecordOf(tc.query)
			a := AsRecord[left](record, "projected")
			remapped := Select(a, schema,
				MapSelected(schema.Status, expr.AsSelected(At(Scope(a), schema.Status))),
				MapSelected(schema.Optional, expr.AsSelected(At(Scope(a), schema.Optional))))
			for _, source := range []Aliased[left, output]{
				a,
				AsRecord[left](CTE("status_cte", record), "projected"),
				AsRecord[left](SelectRecord(a, Scope(a)), "reselected"),
				AsRecord[left](RecordOf(remapped), "remapped"),
			} {
				required := expr.AsSelected(At(Scope(source), schema.Status))
				optional := expr.AsSelected(At(Scope(source), schema.Optional))
				base := SelectScalar(source, required)
				for _, order := range []expr.ProjectionOrder[Alias[left, output]]{
					expr.AscValue(expr.CollateValue(required, expr.CollationC)),
					expr.AscValue(expr.CollateNullableValue(optional, expr.CollationC)),
				} {
					statement, err := CompileRead(ReadScalar(OrderScalar(base, order)))
					if tc.collatable {
						if err != nil || !strings.Contains(statement.SQL(), `COLLATE "C"`) {
							t.Fatal("text projection lost collation", statement.SQL(), err)
						}
					} else if !errors.Is(err, fault.Invalid) {
						t.Fatal("native enum projection accepted COLLATE", err)
					}
				}
				for _, order := range []expr.ProjectionOrder[Alias[left, output]]{
					expr.AscValue(expr.FoundryDefaultCollation(required, expr.CollationUnicode, sqlowner.Expressions())),
					expr.AscValue(expr.FoundryDefaultCollation(optional, expr.CollationUnicode, sqlowner.Expressions())),
				} {
					statement, err := CompileRead(ReadScalar(OrderScalar(base, order)))
					if err != nil || strings.Contains(statement.SQL(), "COLLATE") != tc.collatable {
						t.Fatal("default collation ignored projection capability", statement.SQL(), err)
					}
				}
			}
		})
	}
}

func TestSetCollationCombinesEveryOperand(t *testing.T) {
	type output struct{ Name value.Nullable[string] }
	type fields struct {
		Row[output]
		Name Field[output, value.Nullable[string]]
	}
	schema := Define(func(b Builder[output], r *output) fields {
		return fields{b.Row(), NullableTextOf(b, &r.Name)}
	})
	m := declareMembers()
	source := From(m.Query())
	unmarked := expr.RawWithCodec[member](codec.Nullable(codec.String[string]()), "NULL")
	marked := expr.RawWithCodec[member](codec.Nullable(codec.String[string]().WithoutCollation()), "NULL")
	a := RecordOf(Select(source, schema, MapSelected(schema.Name, unmarked)))
	b := RecordOf(Select(source, schema, MapSelected(schema.Name, marked)))
	for _, combine := range []func(RecordSource[output], RecordSource[output]) SetQuery[output]{Union[output], UnionAll[output], Intersect[output], IntersectAll[output], Except[output], ExceptAll[output]} {
		for _, set := range []SetQuery[output]{combine(a, b), combine(b, a), combine(a, UnionAll(a, b))} {
			selected := expr.AsSelected(At(set.Scope(), schema.Name))
			if _, err := CompileRead(ReadScalar(SelectScalar(set, expr.CollateNullableValue(selected, expr.CollationC)))); !errors.Is(err, fault.Invalid) {
				t.Fatal("record set accepted explicit collation", err)
			}
			order := expr.AscValue(expr.FoundryDefaultCollation(selected, expr.CollationUnicode, sqlowner.Expressions()))
			statement, err := CompileRead(ReadScalar(OrderScalar(SelectScalar(set, selected), order)))
			if err != nil || strings.Contains(statement.SQL(), "COLLATE") {
				t.Fatal("record set acquired collation", statement.SQL(), err)
			}
		}
		plain := combine(a, a)
		if _, err := CompileRead(ReadScalar(SelectScalar(plain, expr.CollateNullableValue(expr.AsSelected(At(plain.Scope(), schema.Name)), expr.CollationC)))); err != nil {
			t.Fatal("set composition mutated its unmarked input", err)
		}
	}
	av, bv := Values(SelectScalar(source, unmarked)), Values(SelectScalar(source, marked))
	for _, combine := range []func(ValueSource[value.Nullable[string]], ValueSource[value.Nullable[string]]) ValueSetQuery[value.Nullable[string]]{
		UnionValues[value.Nullable[string]], UnionAllValues[value.Nullable[string]], IntersectValues[value.Nullable[string]], IntersectAllValues[value.Nullable[string]], ExceptValues[value.Nullable[string]], ExceptAllValues[value.Nullable[string]],
	} {
		for _, set := range []ValueSetQuery[value.Nullable[string]]{combine(av, bv), combine(bv, av), combine(av, UnionAllValues(av, bv))} {
			selected := expr.AsSelected(set.Value())
			if _, err := CompileRead(ReadScalar(SelectScalar(set, expr.CollateNullableValue(selected, expr.CollationC)))); !errors.Is(err, fault.Invalid) {
				t.Fatal("value set accepted explicit collation", err)
			}
			statement, err := CompileRead(ReadScalar(OrderScalar(SelectScalar(set, selected), expr.AscValue(expr.FoundryDefaultCollation(selected, expr.CollationUnicode, sqlowner.Expressions())))))
			if err != nil || strings.Contains(statement.SQL(), "COLLATE") {
				t.Fatal("value set acquired collation", statement.SQL(), err)
			}
		}
		plain := combine(av, av)
		if _, err := CompileRead(ReadScalar(SelectScalar(plain, expr.CollateNullableValue(expr.AsSelected(plain.Value()), expr.CollationC)))); err != nil {
			t.Fatal("value set mutated its input", err)
		}
	}
	for _, recursive := range []func(string, RecordSource[output], func(RecursiveSelf[output]) RecordSource[output]) CommonTable[output]{RecursiveCTE[output], RecursiveAllCTE[output]} {
		cte := recursive("collation_tree", a, func(self RecursiveSelf[output]) RecordSource[output] {
			alias := AsRecord[left](self, "self")
			return RecordOf(Select(alias, schema, MapSelected(schema.Name, expr.RawWithCodec[Alias[left, output]](codec.Nullable(codec.String[string]().WithoutCollation()), "NULL"))))
		})
		alias := AsRecord[left](cte, "tree")
		selected := expr.AsSelected(At(Scope(alias), schema.Name))
		if _, err := CompileRead(ReadScalar(SelectScalar(alias, expr.CollateNullableValue(selected, expr.CollationC)))); !errors.Is(err, fault.Invalid) {
			t.Fatal("recursive output lost step capability", err)
		}
		statement, err := CompileRead(ReadScalar(OrderScalar(SelectScalar(alias, selected), expr.AscValue(expr.FoundryDefaultCollation(selected, expr.CollationUnicode, sqlowner.Expressions())))))
		if err != nil || strings.Contains(statement.SQL(), "COLLATE") {
			t.Fatal("recursive output acquired collation", statement.SQL(), err)
		}
	}
}

func TestModelSetRetainsOtherOperandsCollationMark(t *testing.T) {
	type model struct {
		ID   int64
		Name string
	}
	type table struct {
		orm.Table[model, int64]
		Name orm.Text[model, string]
	}
	declare := func(c codec.Codec[string]) table {
		return orm.Define("model_collations", func(b orm.Builder[model], r *model) table {
			return table{orm.TableOf(b, &r.ID), orm.TextOf(b, &r.Name, orm.Codec(c))}
		})
	}
	plain, marked := declare(codec.String[string]()), declare(codec.String[string]().WithoutCollation())
	set := Union(ModelRecord(plain.Query()), ModelRecord(marked.Query()))
	name := expr.AsSelected(expr.Row(expr.At(set.Scope(), orm.TextExpr(plain.Name))))
	if _, err := CompileRead(ReadScalar(SelectScalar(set, expr.CollateValue(name, expr.CollationC)))); !errors.Is(err, fault.Invalid) {
		t.Fatal("model set lost the other operand's mark", err)
	}
	statement, err := CompileRead(ReadScalar(OrderScalar(SelectScalar(set, name), expr.AscValue(expr.FoundryDefaultCollation(name, expr.CollationUnicode, sqlowner.Expressions())))))
	if err != nil || strings.Contains(statement.SQL(), "COLLATE") {
		t.Fatal("model set acquired collation", statement.SQL(), err)
	}
}
