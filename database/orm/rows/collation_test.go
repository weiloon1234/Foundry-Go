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
