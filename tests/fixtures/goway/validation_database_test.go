package goway_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"foundry.test/goway/coremodels"
	"foundry.test/goway/models"
	"foundry.test/goway/models/hooks"
	reads "foundry.test/goway/models/reads"
	old "foundry.test/goway/oldmodels"
	oldhooks "foundry.test/goway/oldmodels/hooks"
	oldreads "foundry.test/goway/oldmodels/reads"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/validation"
	dbvalidation "github.com/weiloon1234/Foundry-Go/validation/database"
)

type observation[V comparable] interface {
	Validate() error
	Exists(context.Context, database.Executor, V) (bool, error)
	AllExist(context.Context, database.Executor, []V) (bool, error)
	AnyExist(context.Context, database.Executor, []V) (bool, error)
}

// Only the baseline needs to adapt the old query lookup to the base rule contract.
type oldValidationLookup[V comparable] struct {
	observation[V]
	db database.Executor
}

func (l oldValidationLookup[V]) Exists(ctx context.Context, v V) (bool, error) {
	return l.observation.Exists(ctx, l.db, v)
}
func (l oldValidationLookup[V]) AllExist(ctx context.Context, v []V) (bool, error) {
	return l.observation.AllExist(ctx, l.db, v)
}
func (l oldValidationLookup[V]) AnyExist(ctx context.Context, v []V) (bool, error) {
	return l.observation.AnyExist(ctx, l.db, v)
}

func compareObservation[V comparable](t *testing.T, ctx context.Context, legacy, current observation[V], input V) {
	t.Helper()
	for _, found := range []bool{true, false} {
		captures := []*goldenCapture{{}, {}}
		for i, l := range []observation[V]{legacy, current} {
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
		if !oracleEqual(t, "validation_database/1", captures[0].output.String(), captures[1].output.String()) {
			t.Fatal(goldenDiff(captures[0].output.String(), captures[1].output.String()))
		}
	}
}

func TestValidationDatabaseObservationParity(t *testing.T) {
	compareObservation(t, t.Context(), query.Lookup(old.QueryUsers().Where(old.UserFields().Status.Eq(old.StatusActive)), old.UserFields().Email), orm.Lookup(models.Users.Query().Where(models.Users.Status.Eq(models.StatusActive)), models.Users.Email), "literal_%!'@example.test")
	compareObservation(t, t.Context(), query.Lookup(old.QueryUsers(), old.UserFields().Nickname), orm.Lookup(models.Users.Query(), models.Users.Nickname), "member")
	amount := must(decimal.Parse("9007199254740993.123456789012345678901"))
	compareObservation(t, t.Context(), query.Lookup(old.QueryLedgerEntries(), old.LedgerEntryFields().Amount), orm.Lookup(models.LedgerEntries.Query(), models.LedgerEntries.Amount), amount)
	compareObservation(t, t.Context(), query.Lookup(oldhooks.QueryHookMembers(), oldhooks.MemberFields().Note), orm.Lookup(hooks.Members.Query(), hooks.Members.Note), coremodels.StoredLabel("stored"))
	compareObservation(t, t.Context(), query.Lookup(oldhooks.QueryHookMembers().WithTrashed(), oldhooks.MemberFields().Name), orm.Lookup(hooks.Members.Query().WithTrashed(), hooks.Members.Name), "name")
}

func TestValidationDatabaseGlobalScopesWithoutRetrieval(t *testing.T) {
	for _, tenant := range []int64{7, 19} {
		trace := []string{}
		ctx := coremodels.LifecycleContext(coremodels.TenantContext(t.Context(), tenant), &trace)
		compareObservation(t, ctx, query.Lookup(oldreads.QueryReadMembers(), oldreads.MemberFields().Name), orm.Lookup(reads.Members.Query(), reads.Members.Name), "member")
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
		legacy := oldValidationLookup[string]{query.Lookup(old.QueryUsers(), old.UserFields().Email), db}
		current := dbvalidation.Lookup(db, models.Users.Query(), models.Users.Email)
		pairs := [][2]validation.Rule[string]{{validation.Exists(legacy), dbvalidation.Exists(db, models.Users.Query(), models.Users.Email)}, {validation.Unique(legacy), dbvalidation.Unique(db, models.Users.Query(), models.Users.Email)}}
		for _, pair := range pairs {
			before := g.statements
			if !reflect.DeepEqual(must(pair[0].Description()), must(pair[1].Description())) || !oracleEqual(t, "validation_database/value1", oracleSnapshot(must(pair[0].Description())), oracleSnapshot(must(pair[1].Description()))) {
				t.Fatal("rule metadata changed")
			}
			if g.statements != before {
				t.Fatal("description performed IO")
			}
			a, b := pair[0].Check(t.Context(), "secret", validation.DefaultLimits()), pair[1].Check(t.Context(), "secret", validation.DefaultLimits())
			if !reflect.DeepEqual(validationIssues(a), validationIssues(b)) || !oracleEqual(t, "validation_database/value2", oracleSnapshot(validationIssues(a)), oracleSnapshot(validationIssues(b))) {
				t.Fatalf("rule errors changed: %v / %v", a, b)
			}
		}
		for _, pair := range [][2]validation.Rule[[]string]{{validation.ExistsAll[[]string](legacy), validation.ExistsAll[[]string](current)}, {validation.UniqueAll[[]string](legacy), validation.UniqueAll[[]string](current)}} {
			if !reflect.DeepEqual(must(pair[0].Description()), must(pair[1].Description())) || !oracleEqual(t, "validation_database/value1_later", oracleSnapshot(must(pair[0].Description())), oracleSnapshot(must(pair[1].Description()))) {
				t.Fatal("batch metadata changed")
			}
			a, b := pair[0].Check(t.Context(), []string{"secret", "secret"}, validation.DefaultLimits()), pair[1].Check(t.Context(), []string{"secret", "secret"}, validation.DefaultLimits())
			if !reflect.DeepEqual(validationIssues(a), validationIssues(b)) || !oracleEqual(t, "validation_database/value2_later", oracleSnapshot(validationIssues(a)), oracleSnapshot(validationIssues(b))) {
				t.Fatalf("batch errors changed: %v / %v", a, b)
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

func TestValidationDatabaseRejectsBadSourcesAndRedacts(t *testing.T) {
	g := &goldenCapture{}
	db := g.open(t.Context())
	defer db.Close(t.Context())
	for _, q := range []orm.Query[models.User]{{}, models.Users.Query().Limit(1), models.Users.Query().Offset(1), models.Users.Query().OrderBy(models.Users.ID.Asc()), orm.WithRelationLimits(models.Users.Query(), orm.DefaultRelationLimits())} {
		if dbvalidation.Unique(db, q, models.Users.Email).Validate() == nil {
			t.Fatal("invalid source accepted")
		}
	}
	type fake struct {
		orm.KeyColumn[models.User, string]
	}
	for _, column := range []orm.KeyColumn[models.User, string]{nil, orm.Text[models.User, string]{}, fake{}, fake{models.Users.Email}} {
		if dbvalidation.Exists(db, models.Users.Query(), column).Validate() == nil {
			t.Fatal("invalid column accepted")
		}
	}
	var zero dbvalidation.ModelLookup[models.User, string]
	if _, err := zero.Exists(t.Context(), "secret"); err == nil {
		t.Fatal("zero lookup accepted")
	}
	scopeErr := errors.New("scope unavailable")
	scoped := dbvalidation.Scoped(db, func(context.Context) (orm.Query[models.User], error) { return models.Users.Query(), scopeErr }, models.Users.Email)
	if _, err := scoped.Exists(t.Context(), "secret"); !errors.Is(err, scopeErr) {
		t.Fatal("scope failure became an observation")
	}
	if g.statements != 0 {
		t.Fatal("invalid sources performed IO")
	}
	const secret = "retained-private-tenant"
	q := models.Users.Query().Where(models.Users.Email.Eq(secret))
	a, b := orm.Lookup(q, models.Users.Email), dbvalidation.Lookup(db, q, models.Users.Email)
	for _, v := range []any{a, b, struct {
		lookup orm.ValueLookup[models.User, string]
	}{a}, struct {
		lookup dbvalidation.ModelLookup[models.User, string]
	}{b}, struct{ nested any }{struct {
		lookup dbvalidation.ModelLookup[models.User, string]
	}{b}}} {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			if strings.Contains(fmt.Sprintf(format, v), secret) || strings.Contains(fmt.Errorf(format, v).Error(), secret) {
				t.Fatal("lookup leaked private scope")
			}
		}
	}
}

func TestValidationDatabaseCheckTimeScopesAndExclusions(t *testing.T) {
	var bindings []any
	g := &goldenCapture{reply: func(_ string, args []driver.NamedValue) ([]string, [][]driver.Value) {
		bindings = make([]any, len(args))
		for i, arg := range args {
			bindings[i] = arg.Value
		}
		return []string{"exists"}, [][]driver.Value{{false}}
	}}
	db := g.open(t.Context())
	defer db.Close(t.Context())
	slot := validation.NewSlot[int64]()
	// Both the retained scope and the request-specific key exclusion must survive.
	ignored := dbvalidation.UniqueIgnoring(db, reads.Members.Query().Where(reads.Members.Name.Ne("tenant-private")), reads.Members.Name, reads.Members.ID, slot)
	if ignored.Validate() == nil {
		t.Fatal("missing provider accepted")
	}
	type rename struct {
		Key  int64
		Name string
	}
	name := validation.DefineField("name", func(v rename) string { return v.Name })
	rule := validation.Provide(slot, func(_ context.Context, v rename) (int64, error) { return v.Key, nil }, name.Rules(ignored))
	for _, key := range []int64{17, 29} {
		g.output.Reset()
		g.sql.Reset()
		if err := rule.Check(coremodels.TenantContext(t.Context(), key), rename{key, "candidate"}, validation.DefaultLimits()); err != nil {
			t.Fatal(err)
		}
		sql := g.sql.String()
		if !strings.Contains(sql, `"name" <>`) || !strings.Contains(sql, `"id" <>`) || !strings.Contains(sql, `"tenant_id" =`) || !reflect.DeepEqual(bindings, []any{"tenant-private", key, "candidate", key, int64(1)}) {
			t.Fatalf("scope/exclusion missing: %s", sql)
		}
	}
	calls := 0
	scoped := dbvalidation.Scoped(db, func(ctx context.Context) (orm.Query[reads.Member], error) {
		calls++
		tenant, err := coremodels.TenantID(ctx)
		return reads.Members.Query().Where(reads.Members.ID.Ne(tenant)), err
	}, reads.Members.Name)
	for _, tenant := range []int64{7, 19} {
		_, err := scoped.Exists(coremodels.TenantContext(t.Context(), tenant), "candidate")
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 || g.statements != 4 {
		t.Fatalf("scope callbacks/statements %d/%d", calls, g.statements)
	}
	before := g.statements
	if _, err := scoped.Exists(t.Context(), "candidate"); err == nil || g.statements != before {
		t.Fatal("missing tenant authorized a lookup")
	}
}
