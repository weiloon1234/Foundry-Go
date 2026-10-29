package pgsmoke_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type adjustmentAccount struct {
	ID        int64
	Balance   int64
	TenantID  int64
	DeletedAt value.Nullable[time.Time]
}
type adjustmentAccounts struct {
	orm.Table[adjustmentAccount, int64]
	Balance   orm.Number[adjustmentAccount, int64]
	TenantID  orm.Number[adjustmentAccount, int64]
	DeletedAt orm.SoftDelete[adjustmentAccount, time.Time]
}
type adjustmentTenant struct{}

// Compiled by lane B; the lead runs against PostgreSQL in a retained namespace.
func TestAdjustmentVisibilityAndLifecycle(t *testing.T) {
	for _, mode := range []string{"plain", "hooks"} {
		t.Run(mode, func(t *testing.T) {
			statements, callbacks := 0, 0
			db := postgres.Isolate(t).Open(t, database.WithQueryObserver(func(_ context.Context, e database.QueryEvent) {
				if e.Operation == "query" {
					statements++
				}
			}))
			ctx := context.WithValue(t.Context(), adjustmentTenant{}, int64(7))
			for _, sql := range []string{
				`CREATE TABLE adjustment_accounts(id bigint PRIMARY KEY, balance bigint NOT NULL, tenant_id bigint NOT NULL, deleted_at timestamptz)`,
				`INSERT INTO adjustment_accounts VALUES (1,1,7,NULL),(2,3,7,NULL),(3,1,8,NULL),(4,1,7,now())`,
			} {
				_, err := db.Exec(ctx, sql)
				must(t, err)
			}
			options := []orm.Option{orm.WithGlobalScopes(func(f adjustmentAccounts) []orm.GlobalScope[adjustmentAccount] {
				return []orm.GlobalScope[adjustmentAccount]{orm.Scope(f.Table, "tenant", func(ctx context.Context) (orm.Predicate[adjustmentAccount], error) {
					id, ok := ctx.Value(adjustmentTenant{}).(int64)
					if !ok {
						return orm.Predicate[adjustmentAccount]{}, errors.New("missing tenant")
					}
					return orm.NumberEq(f.TenantID, id), nil
				})}
			})}
			if mode == "hooks" {
				options = append(options, orm.WithHooks(func() orm.Hooks[adjustmentAccount] {
					return orm.Hooks[adjustmentAccount]{Updating: func(context.Context, *database.Tx, adjustmentAccount, *orm.Write[adjustmentAccount]) error {
						callbacks++
						return nil
					}}
				}))
			}
			f := orm.Define("adjustment_accounts", func(b orm.Builder[adjustmentAccount], m *adjustmentAccount) adjustmentAccounts {
				return adjustmentAccounts{orm.TableOf(b, &m.ID), orm.NumberOf(b, &m.Balance), orm.NumberOf(b, &m.TenantID), orm.SoftDeleteOf(b, &m.DeletedAt)}
			}, options...)
			for _, tc := range []struct {
				key  int64
				want error
			}{{1, orm.ErrBoundExceeded}, {99, database.NotFound}, {3, database.NotFound}, {4, database.NotFound}, {2, nil}} {
				statements = 0
				row, err := orm.Update(ctx, db, f.Table.Query(), f.Table.Key(), tc.key, orm.NumberDecrement(f.Balance, 2, orm.AtLeast(int64(0))))
				if !errors.Is(err, tc.want) || tc.want != orm.ErrBoundExceeded && errors.Is(err, orm.ErrBoundExceeded) {
					t.Fatalf("key=%d: %v", tc.key, err)
				}
				wantStatements := 2
				if tc.want == nil && mode == "plain" {
					wantStatements = 1
				}
				if statements != wantStatements || tc.want != nil && callbacks != 0 {
					t.Fatalf("key=%d statements=%d callbacks=%d", tc.key, statements, callbacks)
				}
				if tc.want == nil && row.Balance != 1 {
					t.Fatal("adjusted balance", row.Balance)
				}
			}
			rows, err := db.Query(ctx, `SELECT balance FROM adjustment_accounts ORDER BY id`)
			must(t, err)
			defer rows.Close()
			count := 0
			for rows.Next() {
				var balance int64
				must(t, rows.Scan(&balance))
				if balance != 1 {
					t.Fatal("rejected or hidden balance changed", balance)
				}
				count++
			}
			must(t, rows.Err())
			if count != 4 {
				t.Fatal("persisted row count", count)
			}
		})
	}
}
