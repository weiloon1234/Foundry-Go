package goway_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"foundry.test/goway/coremodels"
	"foundry.test/goway/models"
	reads "foundry.test/goway/models/reads"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/validation"
	dbvalidation "github.com/weiloon1234/Foundry-Go/validation/database"
	"reflect"
	"strings"
	"testing"
)

func TestValidationDatabaseRejectsBadSourcesAndRedacts(t *testing.T) {
	g := &goldenCapture{}
	db := g.open(t.Context())
	defer db.Close(t.Context())
	for _, q := range []orm.Query[models.User]{{}, models.Users.Query().Limit(1), models.Users.Query().Offset(1), models.Users.Query().OrderBy(orm.KeyAsc(models.Users.ID)), orm.WithRelationLimits(models.Users.Query(), orm.DefaultRelationLimits())} {
		if validation.ValidateRule(dbvalidation.Unique(db, q, orm.TextExpr(models.Users.Email))) == nil {
			t.Fatal("invalid source accepted")
		}
	}
	type fake struct {
		orm.KeyColumn[models.User, string]
	}
	for _, column := range []orm.KeyColumn[models.User, string]{nil, orm.TextExpr(orm.Text[models.User, string]{}), fake{}, fake{orm.TextExpr(models.Users.Email)}} {
		if validation.ValidateRule(dbvalidation.Exists(db, models.Users.Query(), column)) == nil {
			t.Fatal("invalid column accepted")
		}
	}
	var zero dbvalidation.ModelLookup[models.User, string]
	if _, err := zero.Exists(t.Context(), "secret"); err == nil {
		t.Fatal("zero lookup accepted")
	}
	scopeErr := errors.New("scope unavailable")
	scoped := dbvalidation.Scoped(db, func(context.Context) (orm.Query[models.User], error) { return models.Users.Query(), scopeErr }, orm.TextExpr(models.Users.Email))
	if _, err := scoped.Exists(t.Context(), "secret"); !errors.Is(err, scopeErr) {
		t.Fatal("scope failure became an observation")
	}
	if g.statements != 0 {
		t.Fatal("invalid sources performed IO")
	}
	const secret = "retained-private-tenant"
	q := models.Users.Query().Where(orm.TextEq(models.Users.Email, secret))
	a, b := orm.Lookup(q, orm.TextExpr(models.Users.Email)), dbvalidation.Lookup(db, q, orm.TextExpr(models.Users.Email))
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
	ignored := dbvalidation.UniqueIgnoring(db, reads.Members.Query().Where(orm.TextNe(reads.Members.Name, "tenant-private")), orm.TextExpr(reads.Members.Name), orm.KeyExpr(reads.Members.ID), func(id int64) orm.Predicate[reads.Member] { return orm.KeyNe(reads.Members.ID, id) }, slot)
	if validation.ValidateRule(ignored) == nil {
		t.Fatal("missing provider accepted")
	}
	type rename struct {
		Key  int64
		Name string
	}
	name := validation.DefineField("name", func(v rename) string { return v.Name })
	rule := validation.Provide(slot, func(_ context.Context, v rename) (int64, error) { return v.Key, nil }, validation.Rules(name, ignored))
	for _, key := range []int64{17, 29} {
		g.output.Reset()
		g.sql.Reset()
		if err := validation.Check(rule, coremodels.TenantContext(t.Context(), key), rename{key, "candidate"}, validation.DefaultLimits()); err != nil {
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
		return reads.Members.Query().Where(orm.KeyNe(reads.Members.ID, tenant)), err
	}, orm.TextExpr(reads.Members.Name))
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
