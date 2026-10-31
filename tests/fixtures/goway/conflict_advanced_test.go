package goway_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	n "foundry.test/goway/models"
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	r "github.com/weiloon1234/Foundry-Go/database/orm/rows"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func anotherConflictDeclaration() n.WriteRecordTable {
	return orm.Define("write_records", func(b orm.Builder[n.WriteRecord], m *n.WriteRecord) n.WriteRecordTable {
		t := orm.TableOf(b, &m.Code)
		return n.WriteRecordTable{Table: t, Code: orm.NumberKey(t.Key()), Name: orm.TextOf(b, &m.Name), Enabled: orm.ColumnOf(b, &m.Enabled), Note: orm.NullableTextOf(b, &m.Note), Amount: orm.NumberOf(b, &m.Amount)}
	})
}

func TestAdvancedConflictInvalidPoliciesSendNoSQL(t *testing.T) {
	f, other := n.WriteRecords, anotherConflictDeclaration()
	records := r.ConflictRowsOf(f.Query())
	foreign := r.ConflictRowsOf(other.Query())
	valid := orm.OnConflict(orm.ConflictKey(orm.TextExpr(f.Name))).DoUpdate(orm.ConflictIncoming(orm.TextExpr(f.Name)))
	inner := r.SelectScalar(r.From(f.Query()), expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(f.Amount)))))
	computed := expr.Group(expr.Add(expr.Value(orm.NumberExpr(f.Amount)), expr.Value(orm.NumberExpr(f.Amount))))
	for name, p := range map[string]orm.Conflict[n.WriteRecord]{
		"foreign target":           r.OnConflictKeys(expr.Group(expr.Value(orm.TextExpr(other.Name)))).DoNothing(),
		"foreign calculation":      valid.DoUpdate(r.SetConflictValue(orm.NumberExpr(f.Amount), expr.At(foreign.Proposed(), orm.NumberExpr(other.Amount)))),
		"foreign condition":        r.ConflictWhereRows(valid, expr.Eq(expr.At(foreign.Proposed(), orm.ColumnExpr(other.Enabled)), true)),
		"foreign partial index":    r.ConflictTargetWhere(valid, expr.Eq(expr.Value(orm.ColumnExpr(other.Enabled)), true)),
		"zero calculation":         valid.DoUpdate(r.SetConflictValue(orm.NumberExpr(f.Amount), expr.RowExpression[r.ConflictRow[n.WriteRecord], decimal.Decimal]{})),
		"zero rows":                valid.DoUpdate(r.SetConflictValue(orm.NumberExpr(f.Amount), expr.At(r.ConflictRows[n.WriteRecord]{}.Stored(), orm.NumberExpr(f.Amount)))),
		"duplicate index":          r.OnConflictKeys(computed, computed).DoNothing(),
		"scalar index":             r.OnConflictKeys(expr.Group(r.ScalarRowQuery(r.From(f.Query()), inner))).DoNothing(),
		"subquery index condition": r.ConflictTargetWhere(valid, r.ExistsQuery(r.From(f.Query()), r.From(f.Query()))),
		"named partial":            orm.OnConflictConstraint[n.WriteRecord]("write_records_pkey").TargetWhere(orm.ColumnEq(f.Enabled, true)).DoNothing(),
		"do nothing condition":     r.ConflictWhereRows(orm.OnConflict(orm.ConflictKey(orm.TextExpr(f.Name))).DoNothing(), expr.Eq(expr.At(records.Proposed(), orm.ColumnExpr(f.Enabled)), true)),
	} {
		t.Run(name, func(t *testing.T) {
			capture := &goldenCapture{}
			db, err := database.Prepare(database.Adapter{Connector: connector{capture}}, database.DefaultPoolConfig())
			if err != nil {
				t.Fatal(err)
			}
			check(db.Start(t.Context()))
			t.Cleanup(func() { check(db.Close(context.Background())) })
			_, err = orm.Upsert(t.Context(), db, f.Query(), p, orm.TextSet(f.Name, "input"))
			if !errors.Is(err, fault.Invalid) || capture.statements != 0 {
				t.Fatal(err, capture.output.String())
			}
		})
	}
	// A calculation cannot bypass a Go mutator, even when its stored type matches.
	hf := h.Members
	hr := r.ConflictRowsOf(hf.Query())
	capture := &goldenCapture{}
	db, err := database.Prepare(database.Adapter{Connector: connector{capture}}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	check(db.Start(t.Context()))
	t.Cleanup(func() { check(db.Close(context.Background())) })
	p := orm.OnConflict(orm.ConflictKey(orm.KeyExpr(hf.Key()))).DoUpdate(r.SetConflictValue(orm.InputTextExpr(hf.Name), expr.At(hr.Stored(), orm.InputTextExpr(hf.Name))))
	_, err = orm.Upsert(t.Context(), db, hf.Query(), p, orm.KeySet(hf.Key(), 1), orm.InputTextSet(hf.Name, "input"), orm.ColumnSet(hf.Body, []byte{1}))
	if !errors.Is(err, fault.Invalid) || capture.statements != 0 {
		t.Fatal(err, capture.output.String())
	}
	// The same field's proposed value has already passed its Go mutator.
	capture.scriptedColumns = []string{"id", "name", "note", "body", "created_at", "updated_at", "deleted_at"}
	now := time.Now().UTC()
	capture.scriptedBatches = [][][]driver.Value{{{int64(1), "input", nil, []byte{1}, now, now, nil}}}
	p = p.DoUpdate(r.SetConflictValue(orm.InputTextExpr(hf.Name), expr.At(hr.Proposed(), orm.InputTextExpr(hf.Name))))
	got, err := orm.Upsert(t.Context(), db, hf.Query(), p, orm.KeySet(hf.Key(), 1), orm.InputTextSet(hf.Name, " input "), orm.ColumnSet(hf.Body, []byte{1}))
	if err != nil || !got.IsSet() || !strings.Contains(capture.sql.String(), `"name" = "excluded"."name"`) {
		t.Fatal(got, err, capture.output.String())
	}
}

type advancedScopeKey struct{}
type advancedScoped struct {
	ID     int64
	Tenant int64
	Amount int64
	Note   value.Nullable[int64]
}
type advancedScopedTable struct {
	orm.Table[advancedScoped, int64]
	Tenant, Amount orm.Number[advancedScoped, int64]
	Note           orm.NullableNumber[advancedScoped, int64]
}

func advancedScopedDeclaration(name string, scoped bool) advancedScopedTable {
	options := []orm.Option{}
	if scoped {
		options = append(options, orm.WithGlobalScopes(func(t advancedScopedTable) []orm.GlobalScope[advancedScoped] {
			return []orm.GlobalScope[advancedScoped]{orm.Scope(t.Table, "tenant", func(ctx context.Context) (orm.Predicate[advancedScoped], error) {
				tenant, ok := ctx.Value(advancedScopeKey{}).(int64)
				if !ok {
					return orm.Predicate[advancedScoped]{}, errors.New("tenant required")
				}
				return orm.NumberEq(t.Tenant, tenant), nil
			})}
		}))
	}
	return orm.Define(name, func(b orm.Builder[advancedScoped], m *advancedScoped) advancedScopedTable {
		return advancedScopedTable{orm.TableOf(b, &m.ID), orm.NumberOf(b, &m.Tenant), orm.NumberOf(b, &m.Amount), orm.NullableNumberOf(b, &m.Note)}
	}, options...)
}
func TestAdvancedConflictSubqueryScopesFailClosed(t *testing.T) {
	dest := advancedScopedDeclaration("conflict_dest", false)
	source := advancedScopedDeclaration("conflict_source", true)
	records := r.ConflictRowsOf(dest.Query())
	scalar := r.SelectScalar(r.From(source.Query().Limit(1)), expr.AsSelected(expr.Row(expr.Value(orm.NumberExpr(source.Amount)))))
	policy := orm.OnConflict(orm.ConflictKey(orm.KeyExpr(dest.Key()))).DoUpdate(r.SetConflictValue(orm.NullableNumberExpr(dest.Note), r.ConflictScalarRowQuery(records, scalar)))
	capture := &goldenCapture{scriptedColumns: []string{"id", "tenant", "amount", "note"}, scriptedBatches: [][][]driver.Value{{{int64(1), int64(9), int64(3), int64(7)}}}}
	db, err := database.Prepare(database.Adapter{Connector: connector{capture}}, database.DefaultPoolConfig())
	if err != nil {
		t.Fatal(err)
	}
	check(db.Start(t.Context()))
	t.Cleanup(func() { check(db.Close(context.Background())) })
	draft := []orm.Assignment[advancedScoped]{orm.KeySet(dest.Key(), 1), orm.NumberSet(dest.Tenant, 9), orm.NumberSet(dest.Amount, 3)}
	_, err = orm.Upsert(t.Context(), db, dest.Query(), policy, draft...)
	if err == nil || capture.statements != 0 {
		t.Fatal("missing scope executed", err, capture.output.String())
	}
	ctx := context.WithValue(t.Context(), advancedScopeKey{}, int64(9))
	item, err := orm.Upsert(ctx, db, dest.Query(), policy, draft...)
	if err != nil || !item.IsSet() || !strings.Contains(capture.sql.String(), `"conflict_source"."tenant" =`) {
		t.Fatal(item, err, capture.output.String())
	}
}
