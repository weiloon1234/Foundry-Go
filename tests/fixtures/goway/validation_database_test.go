package goway_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"foundry.test/goway/coremodels"
	"foundry.test/goway/models"
	hooks "foundry.test/goway/models/hooks"
	reads "foundry.test/goway/models/reads"
	"github.com/weiloon1234/Foundry-Go/database/orm"

	"github.com/weiloon1234/Foundry-Go/database"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/validation"
	dbvalidation "github.com/weiloon1234/Foundry-Go/validation/database"

	"testing"
)

type observation[V comparable] interface {
	Validate() error
	Exists(context.Context, database.Executor, V) (bool, error)
	AllExist(context.Context, database.Executor, []V) (bool, error)
	AnyExist(context.Context, database.Executor, []V) (bool, error)
}

func compareObservation[V comparable](t *testing.T, ctx context.Context, current observation[V], input V) {
	t.Helper()
	for _, found := range []bool{true, false} {
		captures := []*goldenCapture{{}, {}}
		for i, l := range []observation[V]{current} {
			g := captures[i]
			g.reply = func(string, []driver.NamedValue) ([]string, [][]driver.Value) {
				return []string{"exists"}, [][]driver.Value{{found}}
			}
			db := g.open(ctx)
			func() {
				defer db.Close(ctx)
				before := g.statements
				a, e := l.AllExist(ctx, db, nil)
				if e != nil || !a {
					t.Fatalf("empty all: %v %v", a, e)
				}
				a, e = l.AnyExist(ctx, db, nil)
				if e != nil || a || g.statements != before {
					t.Fatalf("empty any: %v %v", a, e)
				}
				a, e = l.Exists(ctx, db, input)
				if e != nil || a != found {
					t.Fatalf("exists: %v %v", a, e)
				}
				values := make([]V, 130)
				for n := range values {
					values[n] = input
				}
				a, e = l.AllExist(ctx, db, values)
				if e != nil || a != found {
					t.Fatalf("all: %v %v", a, e)
				}
				a, e = l.AnyExist(ctx, db, values)
				if e != nil || a != found {
					t.Fatalf("any: %v %v", a, e)
				}
				// One scalar plus three batches on one path and one short-circuit batch on the other.
				if g.statements != 5 {
					t.Fatalf("statements=%d, want 5", g.statements)
				}
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				_, e = l.AllExist(canceled, db, values)
				if !errors.Is(e, context.Canceled) || g.statements != 5 {
					t.Fatal("cancellation performed IO")
				}
				if _, e = l.AnyExist(nil, db, nil); e == nil {
					t.Fatal("nil context accepted")
				}
			}()
		}
		if !oracleEqual(t, "validation_database/1", captures[0].output.String()) {
			t.Fatal("frozen legacy comparison differs")
		}
	}
}

func TestValidationDatabaseObservationParity(t *testing.T) {
	compareObservation(t, t.Context(), orm.Lookup(models.Users.Query().Where(orm.EnumEq(models.Users.Status, models.StatusActive)), orm.TextExpr(models.Users.Email)), "literal_%!'@example.test")
	compareObservation(t, t.Context(), orm.Lookup(models.Users.Query(), orm.NullableTextExpr(models.Users.Nickname)), "member")
	amount := must(decimal.Parse("9007199254740993.123456789012345678901"))
	compareObservation(t, t.Context(), orm.Lookup(models.LedgerEntries.Query(), orm.NumberExpr(models.LedgerEntries.Amount)), amount)
	compareObservation(t, t.Context(), orm.Lookup(hooks.Members.Query(), orm.NullableInputTextExpr(hooks.Members.Note)), coremodels.StoredLabel("stored"))
	compareObservation(t, t.Context(), orm.Lookup(hooks.Members.Query().WithTrashed(), orm.InputTextExpr(hooks.Members.Name)), "name")
}
func TestValidationDatabaseGlobalScopesWithoutRetrieval(t *testing.T) {
	for _, tenant := range []int64{7, 19} {
		trace := []string{}
		ctx := coremodels.LifecycleContext(coremodels.TenantContext(t.Context(), tenant), &trace)
		compareObservation(t, ctx, orm.Lookup(reads.Members.Query(), orm.TextExpr(reads.Members.Name)), "member")
		if len(trace) != 0 {
			t.Fatalf("observation ran model lifecycle: %v", trace)
		}
	}
}
func TestValidationDatabaseRulesAndDescriptions(t *testing.T) {
	g := &goldenCapture{}
	db := g.open(t.Context())
	defer db.Close(t.Context())
	for _, found := range []bool{false, true} {
		g.reply = func(string, []driver.NamedValue) ([]string, [][]driver.Value) {
			return []string{"exists"}, [][]driver.Value{{found}}
		}
		current := dbvalidation.Lookup(db, models.Users.Query(), orm.TextExpr(models.Users.Email))
		for _, rule := range []validation.Rule[string]{dbvalidation.Exists(db, models.Users.Query(), orm.TextExpr(models.Users.Email)), dbvalidation.Unique(db, models.Users.Query(), orm.TextExpr(models.Users.Email))} {
			before := g.statements
			if !oracleEqual(t, "validation_database/value1", oracleSnapshot(must(validation.DescribeRule(rule)))) {
				t.Fatal("rule metadata changed")
			}
			if g.statements != before {
				t.Fatal("description performed IO")
			}
			checked := validation.Check(rule, t.Context(), "secret", validation.DefaultLimits())
			if !oracleEqual(t, "validation_database/value2", oracleSnapshot(validationIssues(checked))) {
				t.Fatal("rule errors changed")
			}
		}
		for _, rule := range []validation.Rule[[]string]{validation.ExistsAll[[]string](current), validation.UniqueAll[[]string](current)} {
			if !oracleEqual(t, "validation_database/value1_later", oracleSnapshot(must(validation.DescribeRule(rule)))) {
				t.Fatal("batch metadata changed")
			}
			checked := validation.Check(rule, t.Context(), []string{"secret", "secret"}, validation.DefaultLimits())
			if !oracleEqual(t, "validation_database/value2_later", oracleSnapshot(validationIssues(checked))) {
				t.Fatal("batch errors changed")
			}
		}
	}
}
func validationIssues(err error) any {
	if err == nil {
		return nil
	}
	var rejected *validation.Errors
	if errors.As(err, &rejected) {
		return rejected.Issues()
	}
	return err.Error()
}
