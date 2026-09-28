package query

import (
	"context"
	"errors"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/relation"
	"github.com/weiloon1234/Foundry-Go/fault"
	"strings"
	"testing"
)

type uniqueExecutor struct {
	database.Executor
	called int
	sql    string
	args   []any
	cause  error
}

func (e *uniqueExecutor) Query(_ context.Context, sql string, args ...any) (*database.Rows, error) {
	e.called++
	e.sql = sql
	e.args = append([]any(nil), args...)
	return nil, e.cause
}
func TestUniqueLookupMetadataAndParameterizedFailure(t *testing.T) {
	q := cursorQuery()
	field := NewOrderedField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	for _, source := range []Query[cursorRecord]{q.Limit(1), q.Limit(0), q.Offset(1), For[cursorRecord]("records"), {}} {
		if err := Unique(source, field).Validate(); err == nil {
			t.Fatal("scope could conceal duplicates")
		}
	}
	if err := Unique(q, NewScalarField[cursorRecord, int64]("records", "missing", codec.Signed[int64]())).Validate(); err == nil {
		t.Fatal("unstored field accepted")
	}
	if err := Unique(q, NewScalarField[cursorRecord, int64]("foreign", "id", codec.Signed[int64]())).Validate(); err == nil {
		t.Fatal("foreign declaration accepted")
	}
	if err := Unique[cursorRecord, int64](nil, field).Validate(); err == nil {
		t.Fatal("nil source accepted")
	}
	if err := Unique[cursorRecord, int64](q, nil).Validate(); err == nil {
		t.Fatal("nil field accepted")
	}
	lookup := Unique(q.Where(field.Gt(0)).OrderBy(field.Desc()), field)
	if err := lookup.Validate(); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("executor failure")
	executor := &uniqueExecutor{cause: sentinel}
	got, err := lookup.Find(t.Context(), executor, 847392)
	if got.IsSet() || !errors.Is(err, sentinel) || executor.called != 1 {
		t.Fatal("database failure changed to absence", err)
	}
	if strings.Contains(executor.sql, "847392") || !strings.Contains(executor.sql, "LIMIT") || len(executor.args) == 0 {
		t.Fatal("unbounded or interpolated lookup", executor.sql)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := lookup.Find(ctx, executor, 847392); !errors.Is(err, context.Canceled) || executor.called != 1 {
		t.Fatal("canceled lookup reached SQL")
	}
	if _, err := lookup.Find(nil, executor, 847392); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil context accepted")
	}
	var related RelatedLookup[cursorRecord, cursorRecord, int64]
	if related.Validate() == nil {
		t.Fatal("zero relation lookup accepted")
	}
}

func TestLookupDiagnosticsDoNotExposeScopeValues(t *testing.T) {
	q := cursorQuery()
	field := NewOrderedField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	direct := HasOne(field, field).Bind("child", q, q, func(cursorRecord) relation.One[cursorRecord] { return relation.One[cursorRecord]{} }, func(m cursorRecord, _ relation.One[cursorRecord]) cursorRecord { return m }).Where(field.Eq(847392))
	for _, lookup := range []any{Unique(q.Where(field.Eq(847392)), field), RelatedUnique(direct, field)} {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			if text := fmt.Sprintf(format, lookup); strings.Contains(text, "847392") || strings.Contains(text, "predicates") {
				t.Fatal("lookup diagnostic exposed scoped values")
			}
		}
	}
}
