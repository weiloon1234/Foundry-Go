package ormquerytest

import "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"strings"
	"testing"
)

type uniqueTransferExecutor struct {
	database.Executor
	calls int
	sql   string
	args  []any
	cause error
}

func (e *uniqueTransferExecutor) Query(_ context.Context, s string, args ...any) (*database.Rows, error) {
	e.calls++
	e.sql = s
	e.args = append([]any(nil), args...)
	return nil, e.cause
}
func TestUniqueLookupMetadataAndParameterizedFailure(t *testing.T) {
	d := people()
	field := d.Name
	for _, q := range []orm.Query[relationPerson]{d.Query().Limit(1), d.Query().Limit(0), d.Query().Offset(1), {}} {
		if e := orm.Unique(q, orm.TextExpr(field)).Validate(); e == nil {
			t.Fatal("lookup could hide duplicates", e)
		}
	}
	for _, f := range []orm.KeyColumn[relationPerson, string]{nil, orm.TextExpr(orm.Text[relationPerson, string]{}), orm.TextExpr(people().Name)} {
		if e := orm.Unique(d.Query(), f).Validate(); e == nil {
			t.Fatal("unbound or foreign key", e)
		}
	}
	lookup := orm.Unique(d.Query().Where(orm.TextNe(d.Name, "hidden")).OrderBy(orm.TextDesc(d.Name)), orm.TextExpr(field))
	if e := lookup.Validate(); e != nil {
		t.Fatal(e)
	}
	stop := errors.New("executor failure")
	ex := &uniqueTransferExecutor{cause: stop}
	got, e := lookup.Find(t.Context(), ex, "847392")
	if got.IsSet() || !errors.Is(e, stop) || ex.calls != 1 {
		t.Fatal("database failure became absence", e)
	}
	if strings.Contains(ex.sql, "847392") || !strings.Contains(ex.sql, "LIMIT") || len(ex.args) == 0 {
		t.Fatal("unbounded or interpolated lookup", ex.sql)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, e := lookup.Find(ctx, ex, "847392"); !errors.Is(e, context.Canceled) || ex.calls != 1 {
		t.Fatal("cancelled lookup executed", e)
	}
	if _, e := lookup.Find(nil, ex, "847392"); !errors.Is(e, fault.Invalid) || ex.calls != 1 {
		t.Fatal("nil context executed", e)
	}
	var related orm.RelatedLookup[relationPerson, relationPerson, string]
	if related.Validate() == nil {
		t.Fatal("zero related lookup")
	}
}
