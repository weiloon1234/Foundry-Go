package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/value"
)

type adjustmentRecord struct {
	ID      int64
	Balance int64
}

// Empty set writes report zero and dispatch no per-model callbacks. Single-model
// adjustments retain their distinct NotFound/ErrBoundExceeded contract below.
func TestSetUpdateEmptySelectionContract(t *testing.T) {
	for _, mode := range []string{"plain", "hooks", "observers"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			factory := func() Hooks[adjustmentRecord] { calls++; return Hooks[adjustmentRecord]{} }
			var options []Option
			var declarations []lifecycle.Declaration
			if mode == "hooks" {
				options = append(options, WithHooks(factory))
			}
			if mode == "observers" {
				d, err := NewObserver[adjustmentRecord]("adjustment").Declare(factory)
				if err != nil {
					t.Fatal(err)
				}
				declarations = append(declarations, d)
			}
			type fields struct {
				Table[adjustmentRecord, int64]
				Balance Number[adjustmentRecord, int64]
			}
			f := Define("adjustments", func(b Builder[adjustmentRecord], m *adjustmentRecord) fields {
				return fields{TableOf(b, &m.ID), NumberOf(b, &m.Balance)}
			}, options...)
			var statements []string
			zero := int64(0)
			db := hookDB(t, testConnector{statements: &statements, affected: &zero, columns: []string{"id", "balance"}}, declarations...)
			for _, key := range []int64{1, 2} {
				q := f.Query().Where(f.Key().Eq(key), f.Balance.Gte(2))
				if mode != "plain" {
					q = WithoutModelHooks(q)
				}
				count, err := q.UpdateAll(t.Context(), db, f.Balance.Set(0))
				if err != nil || count != 0 || calls != 0 {
					t.Fatalf("empty set update: count=%d callbacks=%d err=%v", count, calls, err)
				}
				if len(statements) != 1 || !strings.HasPrefix(statements[0], "UPDATE ") || !strings.Contains(statements[0], " >= ") || strings.Contains(statements[0], "RETURNING") {
					t.Fatalf("set update SQL: %v", statements)
				}
				statements = nil
			}
		})
	}
}

type scopedAdjustment struct {
	ID        int64
	Balance   int64
	TenantID  int64
	DeletedAt value.Nullable[time.Time]
}
type scopedAdjustments struct {
	Table[scopedAdjustment, int64]
	Balance   Number[scopedAdjustment, int64]
	TenantID  Number[scopedAdjustment, int64]
	DeletedAt SoftDelete[scopedAdjustment, time.Time]
}

func TestAdjustmentEmptySelectionLifecycle(t *testing.T) {
	for _, mode := range []string{"plain", "hooks", "observers"} {
		for _, scenario := range []string{"success", "bound", "missing", "tenant hidden", "soft deleted", "predicate hidden", "row close failure", "check failure", "check canceled", "canceled"} {
			t.Run(mode+"/"+scenario, func(t *testing.T) {
				calls, stages, begins, closed := 0, 0, 0, 0
				factory := func() Hooks[scopedAdjustment] {
					calls++
					return Hooks[scopedAdjustment]{Updating: func(context.Context, *database.Tx, scopedAdjustment, *Write[scopedAdjustment]) error {
						stages++
						return nil
					}}
				}
				options := []Option{WithGlobalScopes(func(f scopedAdjustments) []GlobalScope[scopedAdjustment] {
					return []GlobalScope[scopedAdjustment]{Scope(f.Table, "tenant", func(ctx context.Context) (Predicate[scopedAdjustment], error) {
						tenant, ok := ctx.Value(scopeTenant{}).(int64)
						if !ok {
							return Predicate[scopedAdjustment]{}, tenantMissing
						}
						return f.TenantID.Eq(tenant), nil
					})}
				})}
				var declarations []lifecycle.Declaration
				if mode == "hooks" {
					options = append(options, WithHooks(factory))
				}
				if mode == "observers" {
					observer, err := NewObserver[scopedAdjustment]("adjustment").Declare(factory)
					if err != nil {
						t.Fatal(err)
					}
					declarations = append(declarations, observer)
				}
				f := Define("adjustments", func(b Builder[scopedAdjustment], r *scopedAdjustment) scopedAdjustments {
					return scopedAdjustments{TableOf(b, &r.ID), NumberOf(b, &r.Balance), NumberOf(b, &r.TenantID), SoftDeleteOf(b, &r.DeletedAt)}
				}, options...)
				ctx, cancel := context.WithCancel(tenantContext(t.Context(), 7))
				defer cancel()
				failure := errors.New("driver failed")
				var statements []string
				connector := testConnector{statements: &statements, onBegin: func() { begins++ }, onClose: func() {
					closed++
					if scenario == "canceled" {
						cancel()
					}
				}}
				if scenario == "row close failure" {
					connector.closeErr = failure
				}
				connector.respondBound = func(sql string, args []driver.NamedValue) ([]string, [][]driver.Value, error) {
					for _, predicate := range []string{`"adjustments"."id" = `, `"adjustments"."tenant_id" = `, `"adjustments"."deleted_at" IS NULL`, `"adjustments"."balance" > `} {
						if !strings.Contains(sql, predicate) {
							t.Errorf("lost visibility predicate %s: %s", predicate, sql)
						}
					}
					tenantBound := false
					for _, arg := range args {
						tenantBound = tenantBound || arg.Value == int64(7)
					}
					if !tenantBound {
						t.Errorf("lost tenant context: %v", args)
					}
					if strings.HasPrefix(sql, "SELECT ") && !strings.Contains(sql, "FOR UPDATE") {
						t.Errorf("lost lock: %s", sql)
					}
					if strings.HasPrefix(sql, `SELECT "adjustments"."id" FROM `) {
						if closed != 1 || len(statements) != 2 || strings.Contains(sql, " >= ") {
							t.Errorf("diagnostic read must follow closed empty result and remove only bound: %v", statements)
						}
						if scenario == "check failure" {
							return nil, nil, failure
						}
						if scenario == "check canceled" {
							return []string{"id"}, nil, nil
						}
						switch scenario {
						case "bound":
							return []string{"id"}, [][]driver.Value{{int64(1)}}, nil
						case "missing", "tenant hidden", "soft deleted", "predicate hidden":
							return []string{"id"}, nil, nil
						default:
							t.Errorf("unexpected diagnostic read: %s", sql)
						}
					}
					if !strings.Contains(sql, " >= ") {
						t.Errorf("lost atomic guard: %s", sql)
					}
					columns := []string{"id", "balance", "tenant_id", "deleted_at"}
					if scenario == "success" {
						balance := int64(3)
						if strings.HasPrefix(sql, "UPDATE ") {
							balance = 1
						}
						return columns, [][]driver.Value{{int64(1), balance, int64(7), nil}}, nil
					}
					return columns, nil, nil
				}
				// Cancellation during the diagnostic result must propagate too.
				if scenario == "check canceled" {
					connector.onClose = func() {
						closed++
						if closed == 2 {
							cancel()
						}
					}
				}
				db := hookDB(t, connector, declarations...)
				row, err := Update(ctx, db, f.Query().Where(f.Balance.Gt(-10)), f.Key(), int64(1), f.Balance.Decrement(2, AtLeast(int64(0))))
				wantStatements, wantFactories := 2, 0
				switch scenario {
				case "success":
					if err != nil || row.Balance != 1 {
						t.Fatalf("adjustment: %#v, %v", row, err)
					}
					wantStatements = 1
					if mode != "plain" {
						wantStatements = 2
						wantFactories = 1
					}
				case "bound":
					if !errors.Is(err, ErrBoundExceeded) || errors.Is(err, database.NotFound) {
						t.Fatalf("bound: %v", err)
					}
				case "row close failure", "check failure":
					if !errors.Is(err, failure) || errors.Is(err, ErrBoundExceeded) || errors.Is(err, database.NotFound) {
						t.Fatalf("driver failure: %v", err)
					}
					if scenario == "row close failure" {
						wantStatements = 1
					}
				case "canceled", "check canceled":
					if !errors.Is(err, context.Canceled) || errors.Is(err, ErrBoundExceeded) || errors.Is(err, database.NotFound) {
						t.Fatalf("cancellation: %v", err)
					}
					if scenario == "canceled" {
						wantStatements = 1
					}
				default:
					if !errors.Is(err, database.NotFound) || errors.Is(err, ErrBoundExceeded) {
						t.Fatalf("hidden/missing: %v", err)
					}
				}
				if len(statements) != wantStatements || begins != 1 || calls != wantFactories || stages != wantFactories {
					t.Fatalf("statements=%v begins=%d factories=%d stages=%d", statements, begins, calls, stages)
				}
				if mode == "plain" && !strings.HasPrefix(statements[0], "UPDATE ") || mode != "plain" && !strings.HasPrefix(statements[0], "SELECT ") {
					t.Fatalf("wrong lifecycle path: %v", statements)
				}
			})
		}
	}
}
