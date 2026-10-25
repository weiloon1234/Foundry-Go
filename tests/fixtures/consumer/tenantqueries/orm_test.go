package tenantqueries_test

import (
	"errors"
	"foundry.test/consumer/tenantqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"reflect"
	"strings"
	"testing"
)

func TestORMDeclarations(t *testing.T) {
	if err := orm.Check(tenantqueries.Documents.Table, tenantqueries.Tags.Table, tenantqueries.DocumentTags.Table); err != nil {
		t.Fatal(err)
	}
}

func TestORMTenantScopesFailClosedAndRemainQueryLocal(t *testing.T) {
	q := tenantqueries.Documents.Query()
	if _, err := orm.Compile(q); !errors.Is(err, fault.Invalid) {
		t.Fatal("missing tenant did not fail closed", err)
	}
	first, err := orm.Compile(orm.WithScopeContext(q, tenantqueries.WithTenant(t.Context(), alpha)))
	if err != nil {
		t.Fatal(err)
	}
	second, err := orm.Compile(orm.WithScopeContext(q, tenantqueries.WithTenant(t.Context(), beta)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first.SQL(), "tenant_id") || !strings.Contains(first.SQL(), "published") || !strings.Contains(first.SQL(), "deleted_at") || reflect.DeepEqual(first.Arguments(), second.Arguments()) {
		t.Fatal("tenant scopes lost ownership or request context")
	}
	unscoped, err := orm.Compile(orm.WithoutGlobalScopes(q))
	if err != nil {
		t.Fatal(err)
	}
	if len(unscoped.Arguments()) != 0 {
		t.Fatal("explicit scope bypass retained tenant parameters")
	}
	if _, err := orm.Compile(q); !errors.Is(err, fault.Invalid) {
		t.Fatal("bypass modified the reusable query", err)
	}
}
