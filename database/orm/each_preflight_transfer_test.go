package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
)

func TestPerModelWriteCompilerRetainsScopesAndPrimaryRules(t *testing.T) {
	d := eachFields(Hooks[eachRow]{})
	scoped := d.Query().Where(d.Name.Ne("excluded"))
	if _, _, err := planEach(scoped.state, querycore.UpdateModel, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := planWrite(scoped.state, querycore.UpdateModel, []Assignment[eachRow]{d.Name.Set("new")}, false); !errors.Is(err, fault.Invalid) {
		t.Fatal("single-model write lost primary requirement", err)
	}
	selected := scoped.state
	selected.ast.Predicates = append(selected.ast.Predicates, eachPrimary(selected, int64(8)))
	p, err := planWrite(selected, querycore.UpdateModel, []Assignment[eachRow]{d.Name.Set("new")}, false)
	if err != nil {
		t.Fatal(err)
	}
	s, err := p.compile()
	if err != nil || !strings.Contains(s.SQLText, `"each_records"."name" <> $2`) || !strings.Contains(s.SQLText, `"each_records"."id" = $3`) {
		t.Fatal("candidate dropped original scope", s, err)
	}
	if _, _, err := planEach(d.Query().state, querycore.DeleteModel, 0); !errors.Is(err, fault.Invalid) {
		t.Fatal("zero limit not rejected", err)
	}
	for _, run := range []func() error{
		func() error {
			_, e := RestoreEach(t.Context(), &noEachWriter{}, d.Query(), SoftDeletable[eachRow]{}, 1)
			return e
		},
		func() error {
			_, e := ForceDeleteEach(t.Context(), &noEachWriter{}, d.Query(), SoftDeletable[eachRow]{}, 1)
			return e
		},
	} {
		if e := run(); !errors.Is(e, fault.Invalid) {
			t.Fatal("ordinary model special write", e)
		}
	}
}
func TestPerModelCreationDefersRequiredFieldsAndOwnsEmptyResult(t *testing.T) {
	d := eachFields(Hooks[eachRow]{})
	writer := &noEachWriter{}
	got, err := InsertEach(t.Context(), writer, d.Table, nil)
	if err != nil || got == nil || len(got) != 0 || writer.calls != 0 {
		t.Fatal(got, err, writer.calls)
	}
	if _, err := InsertEach(t.Context(), writer, d.Table, [][]Assignment[eachRow]{{}}); err == nil || writer.calls != 1 {
		t.Fatal("required fields evaluated before hooks", err, writer.calls)
	}
}
func TestPerModelWriteRejectsNullIdentityBeforeCallbacks(t *testing.T) {
	calls := 0
	d := eachFields(Hooks[eachRow]{Updating: func(context.Context, *database.Tx, eachRow, *Write[eachRow]) error { calls++; return nil }})
	// Malformed codec metadata must not turn SQL NULL into a usable identity.
	// Public primary declarations reject nullable storage independently.
	d.core.primary.get = func(unsafe.Pointer) (driver.Value, error) { return nil, nil }
	db := testDB(t, testConnector{columns: []string{"id", "name"}, values: [][]driver.Value{{int64(1), "a"}}})
	got, err := UpdateEach(t.Context(), db, d.Query(), 1, func(context.Context, *database.Tx, eachRow) ([]Assignment[eachRow], error) {
		calls++
		return []Assignment[eachRow]{d.Name.Set("new")}, nil
	})
	if !errors.Is(err, fault.Invalid) || got != nil || calls != 0 {
		t.Fatal("NULL identity reached lifecycle", got, err, calls)
	}
}
func TestLookupWriteValidationBeforeTransaction(t *testing.T) {
	d := eachFields(Hooks[eachRow]{})
	writer := &noEachWriter{}
	calls := 0
	for _, q := range []Query[eachRow]{{}, d.Query().OrderBy(d.Key().Asc()), d.Query().Limit(1), d.Query().Offset(1), WithRelationLimits(d.Query(), DefaultRelationLimits())} {
		if _, err := FirstOrCreate(t.Context(), writer, q); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid lookup", err)
		}
	}
	if _, err := UpdateOrCreate(t.Context(), writer, d.Query(), nil, nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil callback", err)
	}
	if _, err := FirstOrCreate(nil, writer, d.Query()); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil context", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := FirstOrCreate(ctx, writer, d.Query()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if writer.calls != 0 {
		t.Fatal("invalid lookup acquired transaction")
	}
	_, err := UpdateOrCreate(t.Context(), writer, d.Query(), nil, func(context.Context, *database.Tx, eachRow) ([]Assignment[eachRow], error) { calls++; return nil, nil })
	if err == nil || writer.calls != 1 || calls != 0 {
		t.Fatal("draft ran before branch selection", err, writer.calls, calls)
	}
}
