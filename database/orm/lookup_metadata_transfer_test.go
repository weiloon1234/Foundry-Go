package orm

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
	for _, q := range []Query[relationPerson]{d.Query().Limit(1), d.Query().Limit(0), d.Query().Offset(1), {}} {
		if e := Unique(q, field).Validate(); e == nil {
			t.Fatal("lookup could hide duplicates", e)
		}
	}
	for _, f := range []KeyColumn[relationPerson, string]{nil, Text[relationPerson, string]{}, people().Name} {
		if e := Unique(d.Query(), f).Validate(); e == nil {
			t.Fatal("unbound or foreign key", e)
		}
	}
	lookup := Unique(d.Query().Where(d.Name.Ne("hidden")).OrderBy(d.Name.Desc()), field)
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
	var related RelatedLookup[relationPerson, relationPerson, string]
	if related.Validate() == nil {
		t.Fatal("zero related lookup")
	}
}
