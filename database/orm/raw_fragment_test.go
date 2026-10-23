package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func TestWhereRawKeepsScopesAndTransaction(t *testing.T) {
	r := rawRecords()
	type tenantKey struct{}
	r.core.model.GlobalScopes = []querycore.ScopeDefinition{{Name: "tenant", Resolve: func(ctx context.Context) (querycore.Expression, error) {
		if ctx.Value(tenantKey{}) != int64(7) {
			return nil, errors.New("missing tenant")
		}
		return r.Key().Eq(7).expression, nil
	}}}
	began := false
	calls := 0
	db := testDB(t, testConnector{onBegin: func() { began = true }, respondBound: func(sql string, args []driver.NamedValue) ([]string, [][]driver.Value, error) {
		calls++
		if !strings.Contains(sql, "(name = $1 OR name = $2\n)") || !strings.Contains(sql, `"id" = $3`) || len(args) != 3 || args[2].Value != int64(7) {
			t.Fatal(sql, args)
		}
		return []string{"id", "name"}, [][]driver.Value{{int64(7), "Ada"}}, nil
	}})
	ctx := context.WithValue(t.Context(), tenantKey{}, int64(7))
	q := r.Query().Where(WhereRaw[rawRecord]("name = ? OR name = ?", "Ada", "Grace"))
	if _, err := q.All(ctx, db); err != nil {
		t.Fatal(err)
	}
	err := db.Transaction(ctx, func(tx *database.Tx) error {
		if !began {
			t.Fatal("transaction not begun")
		}
		_, err := q.All(ctx, tx)
		return err
	})
	if err != nil || calls != 2 {
		t.Fatal(calls, err)
	}
}
func TestWhereRawCompositionRetainsOwnership(t *testing.T) {
	r, other := rawRecords(), rawRecords()
	good := WhereRaw[rawRecord]("true")
	bad := good
	bad.rawScope = reflect.TypeFor[int]()
	for _, p := range []Predicate[rawRecord]{good, And(good, r.Key().Eq(1)), And(r.Key().Eq(1), good), Or(good, Not(good))} {
		if err := validateQuery(r.Query().Where(p).state); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []Predicate[rawRecord]{bad, Not(bad), And(bad, r.Key().Eq(1)), And(good, r.Key().Eq(1), other.Key().Eq(1)), And(good, Predicate[rawRecord]{})} {
		if err := validateQuery(r.Query().Where(p).state); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid ownership accepted", err)
		}
	}
}
