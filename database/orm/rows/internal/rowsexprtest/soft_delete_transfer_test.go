package rowsexprtest

import "github.com/weiloon1234/Foundry-Go/database/orm/rows"

import (
	"database/sql/driver"
	"errors"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
	"github.com/weiloon1234/Foundry-Go/value"
	"strings"
	"testing"
	"time"
)

type softTransferRow struct {
	ID        int64
	Name      string
	DeletedAt value.Nullable[time.Time]
}
type softTransferFields struct {
	orm.Table[softTransferRow, int64]
	Name      orm.Text[softTransferRow, string]
	DeletedAt orm.SoftDelete[softTransferRow, time.Time]
}

func TestSoftDeleteScopesReachAliasesAndModelMutations(t *testing.T) {
	d := orm.Define("soft_records", func(b orm.Builder[softTransferRow], m *softTransferRow) softTransferFields {
		return softTransferFields{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Name), orm.SoftDeleteOf(b, &m.DeletedAt)}
	})
	for i, q := range []orm.Query[softTransferRow]{d.Query(), d.Query().WithTrashed(), d.Query().OnlyTrashed()} {
		a := rows.As[left](q, "member")
		s, e := rows.CompileRead(rows.SelectRecord(a, rows.Scope(a)))
		if e != nil || strings.Contains(s.SQL(), `"deleted_at" IS `) != (i != 1) {
			t.Fatal(s.SQL(), e)
		}
		core := orm.FoundrySource(q.Where(orm.KeyEq(d.Key(), 7)), sqlowner.Metadata()).Node
		for _, kind := range []querycore.MutationKind{querycore.UpdateModel, querycore.DeleteModel} {
			model := querycore.ModelDefinition{Table: "soft_records", Primary: "id", Columns: core.Source.Columns, SoftDelete: &querycore.SoftDeleteColumn{Name: "deleted_at"}}
			ast := querycore.QueryCore{Table: "soft_records", Model: &model, SoftDeleteScope: []querycore.SoftDeleteScope{querycore.ActiveRecords, querycore.AllRecords, querycore.TrashedRecords}[i], Predicates: []querycore.Expression{querycore.Comparison{Operand: querycore.FieldRef{Table: "soft_records", Column: "id"}, Operator: querycore.Equal, Values: querycore.RetainComparisonValues([]any{int64(7)}), Bind: func(v any) (driver.Value, error) { return v, nil }}}}
			c, e := ast.WriteCompiler(kind, true, false)
			if e != nil {
				t.Fatal(e)
			}
			var as []querycore.MutationAssignment
			if kind == querycore.UpdateModel {
				as = []querycore.MutationAssignment{{Field: querycore.FieldRef{Table: "soft_records", Column: "name"}, AssignmentValue: querycore.CaptureAssignment(codec.String[string](), "changed")}}
			}
			assigned, e := ast.ValidateMutation(kind, as, &c)
			if e != nil {
				t.Fatal(e)
			}
			s, e := ast.CompileMutation(c, ast.ModelSelect(querycore.ColumnSelections(ast.Table, model.Columns)), kind, as, assigned, nil, false)
			if e != nil || strings.Contains(s.SQLText, `"deleted_at" IS `) != (i != 1) {
				t.Fatal(s.SQLText, e)
			}
		}
	}
	for _, test := range []struct {
		q       orm.LockedQuery[softTransferRow]
		suffix  string
		trashed bool
	}{{orm.Lock(d.Query()).WithTrashed(), "FOR UPDATE", false}, {orm.ForShare(d.Query()).OnlyTrashed().NoWait(), "FOR SHARE NOWAIT", true}} {
		s, e := orm.CompileLocked(test.q)
		if e != nil || !strings.HasSuffix(s.SQL(), test.suffix) || strings.Contains(s.SQL(), " IS NOT NULL") != test.trashed || strings.Contains(s.SQL(), " IS NULL") {
			t.Fatal(s.SQL(), e)
		}
	}
	plain := declareMembers()
	for _, q := range []orm.Query[member]{plain.Query().WithTrashed(), plain.Query().OnlyTrashed()} {
		if _, e := rows.CompileRead(rows.ReadScalar(rows.SelectScalar(rows.From(q), expr.AsSelected(expr.Row(expr.Value(orm.KeyExpr(plain.ID))))))); !errors.Is(e, fault.Invalid) {
			t.Fatal("projection lost visibility failure", e)
		}
	}
}
