package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/value"
)

type adjustmentRecord struct {
	ID      int64
	Balance int64
}

// Legacy adjustments are set writes, even when their predicates select one key.
// Guard rejection and a missing key both produce an affected count of zero;
// neither dispatches model callbacks. This is not a single-model adjustment API.
func TestLegacyAdjustmentEmptySelectionContract(t *testing.T) {
	for _, mode := range []string{"plain", "hooks", "observers"} {
		t.Run(mode, func(t *testing.T) {
			for _, key := range []int64{1, 2} {
				calls := 0
				factory := func() query.WriteHooks[adjustmentRecord] {
					calls++
					return query.WriteHooks[adjustmentRecord]{}
				}
				definition := query.Define("adjustments", "id", []query.Column{{Name: "id"}, {Name: "balance"}},
					func(database.Row) (adjustmentRecord, error) {
						t.Fatal("empty result hydrated a model")
						return adjustmentRecord{}, nil
					},
					query.NewModelField("id", codec.Signed[int64](), func(r adjustmentRecord) int64 { return r.ID }),
					query.NewModelField("balance", codec.Signed[int64](), func(r adjustmentRecord) int64 { return r.Balance }))
				var declarations []lifecycle.Declaration
				if mode == "hooks" {
					definition = definition.WithWriteHooks(factory)
				}
				if mode == "observers" {
					observer, err := lifecycle.NewObserver[adjustmentRecord, query.WriteHooks[adjustmentRecord]]("adjustment").Declare(factory)
					if err != nil {
						t.Fatal(err)
					}
					declarations = append(declarations, observer)
					definition = definition.WithObserverHooks(func(context.Context, lifecycle.Observers) (query.WriteHooks[adjustmentRecord], error) {
						return factory(), nil
					}, false)
				}
				var statements []string
				zero := int64(0)
				db := hookDB(t, testConnector{statements: &statements, affected: &zero, columns: []string{"id", "balance"}}, declarations...)
				id := query.NewExactField[adjustmentRecord, int64]("adjustments", "id", codec.Signed[int64]())
				balance := query.NewExactField[adjustmentRecord, int64]("adjustments", "balance", codec.Signed[int64]())
				// Key 1 represents an existing balance of 1, rejecting subtraction of 2.
				// Key 2 is missing. Both predicates select no row in the driver response.
				q := query.ForModel(definition).Where(id.Eq(key), query.GreaterOrEqual(query.Subtract(balance, query.Param(balance, int64(2))), query.Param(balance, int64(0))))
				set := q
				if mode != "plain" {
					set = set.WithoutModelHooks()
				}
				count, err := set.AdjustAll(t.Context(), db, balance.By(2), true, query.Change[adjustmentRecord]())
				if err != nil || count != 0 {
					t.Fatalf("key %d: count=%d, err=%v", key, count, err)
				}
				if len(statements) != 1 || !strings.HasPrefix(statements[0], "UPDATE ") || !strings.Contains(statements[0], " >= ") || strings.Contains(statements[0], "RETURNING") {
					t.Fatalf("set adjustment changed its guarded SQL: %v", statements)
				}
				statements = nil
				// Ordinary per-model guarded patches use NotFound on both paths. The
				// legacy API cannot assign a numeric adjustment through this terminal.
				_, err = q.Patch(t.Context(), db, query.Change(query.Assign[adjustmentRecord]("adjustments", "balance", codec.Signed[int64](), int64(0))))
				if !errors.Is(err, database.NotFound) {
					t.Fatalf("guarded patch key %d: %v", key, err)
				}
				if len(statements) != 1 || !strings.Contains(statements[0], " >= ") {
					t.Fatalf("guarded patch SQL: %v", statements)
				}
				if mode == "plain" && !strings.HasPrefix(statements[0], "UPDATE ") || mode != "plain" && !strings.HasPrefix(statements[0], "SELECT ") {
					t.Fatalf("wrong lifecycle path: %s: %v", mode, statements)
				}
				if calls != 0 {
					t.Fatal("empty selection invoked factories")
				}
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
