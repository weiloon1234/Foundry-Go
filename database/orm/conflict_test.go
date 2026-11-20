package orm_test

import "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/encrypted"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestUpsertMutatorsNormalizeRowsAndPolicyOnce(t *testing.T) {
	var calls []string
	transform := func(label string) func(string) (string, error) {
		return func(s string) (string, error) { calls = append(calls, label+":"+s); return strings.TrimSpace(s), nil }
	}
	fields := transformFields(transform("first"), transform("last"), transform("maybe"))
	policy := orm.OnConflict(orm.ConflictKey(fields.Key())).DoUpdate(orm.ConflictIncoming(fields.First), orm.ConflictSet(fields.Maybe, " literal "), orm.ConflictNull(fields.Maybe))
	// Duplicate targets are rejected before any callback or SQL.
	begun := 0
	var sqls []string
	var binds [][]any
	closed := 0
	db := testDB(t, testConnector{onBegin: func() { begun++ }, statements: &sqls, bindings: &binds, onClose: func() { closed++ }, columns: []string{"id", "first", "maybe", "last"}, values: [][]driver.Value{{int64(1), "stored", nil, "last"}}})
	rows := [][]orm.Assignment[transformRow]{{orm.Set(fields.Key(), 1), orm.Set(fields.First, " a "), orm.Set(fields.Last, " b ")}, {orm.Set(fields.Key(), 2), orm.Set(fields.First, " c "), orm.Set(fields.Last, " d ")}}
	if _, err := orm.UpsertMany(t.Context(), db, fields.Query(), policy, rows); err == nil || begun != 0 || len(calls) != 0 {
		t.Fatal("duplicate preflight", err, begun, calls)
	}
	policy = orm.OnConflict(orm.ConflictKey(fields.Key())).DoUpdate(orm.ConflictIncoming(fields.First), orm.ConflictSet(fields.Maybe, " literal "))
	for range 2 {
		calls = nil
		got, err := orm.UpsertMany(t.Context(), db, fields.Query(), policy, rows)
		if err != nil || len(got) != 1 || got[0].First != "stored" {
			t.Fatal("returning", got, err)
		}
		if want := []string{"first: a ", "last: b ", "first: c ", "last: d ", "maybe: literal "}; !reflect.DeepEqual(calls, want) {
			t.Fatal("transform ordering/count", calls)
		}
	}
	if len(sqls) != 2 || closed != 2 || !strings.Contains(sqls[0], `"first" = "excluded"."first"`) || !strings.Contains(sqls[0], `"maybe" = $7`) {
		t.Fatal(sqls, closed)
	}
	if !reflect.DeepEqual(binds[0], []any{int64(1), "a", "b", int64(2), "c", "d", "literal"}) || !reflect.DeepEqual(binds[0], binds[1]) {
		t.Fatal(binds)
	}
	calls = nil
	_, err := orm.Upsert(t.Context(), db, fields.Query(), policy.DoUpdate(orm.ConflictNull(fields.Maybe)), rows[0]...)
	if err != nil || !reflect.DeepEqual(calls, []string{"first: a ", "last: b "}) {
		t.Fatal("NULL invoked mutator", calls, err)
	}
}

func TestUpsertSkipsLifecycleButKeepsConventionsAndDistinctInputs(t *testing.T) {
	called := 0
	table := hookedFields(func() orm.Hooks[hookedRow] { called++; return orm.Hooks[hookedRow]{} })
	observer, err := orm.NewObserver[hookedRow]("upsert").Declare(func() orm.Hooks[hookedRow] { called++; return orm.Hooks[hookedRow]{} })
	if err != nil {
		t.Fatal(err)
	}
	var sqls []string
	var binds [][]any
	db := hookDB(t, testConnector{statements: &sqls, bindings: &binds, columns: hookColumns, values: hookValues("stored", "note", nil)}, observer)
	policy := orm.OnConflict(orm.ConflictKey(table.Key())).DoUpdate(orm.ConflictSet(table.Note, hookedInput{" policy "}), orm.ConflictSet(table.Updated, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)))
	got, err := orm.Upsert(t.Context(), db, table.Query(), policy, orm.Set(table.Key(), 1), orm.Set(table.Name, " proposed "), orm.Set(table.Body, []byte{1}), orm.Set(table.Note, hookedInput{" input "}))
	if err != nil || !got.IsSet() || called != 0 {
		t.Fatal("lifecycle dispatch", err, called)
	}
	if len(sqls) != 1 || !strings.Contains(sqls[0], `"updated_at" = "excluded"."updated_at"`) {
		t.Fatal(sqls)
	}
	if !reflect.DeepEqual(binds[0], []any{int64(1), "proposed", "input", []byte{1}, time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC), "policy"}) {
		t.Fatal(binds)
	}
}

func TestUpsertPoliciesRejectBeforeCallbacks(t *testing.T) {
	called := 0
	transform := func(s string) (string, error) { called++; return s, nil }
	a := transformFields(transform, transform, transform)
	b := transformFields(transform, transform, transform)
	policies := []orm.Conflict[transformRow]{
		{}, orm.OnConflict[transformRow]().DoUpdate(orm.ConflictIncoming(a.First)),
		orm.OnConflictConstraint[transformRow]("not a constraint").DoNothing(),
		orm.OnConflict(orm.ConflictKey(a.Key())).DoUpdate(orm.ConflictIncoming(a.Key())),
		orm.OnConflict(orm.ConflictKey(a.Key())).DoUpdate(orm.ConflictIncoming(b.First)),
		orm.OnConflict(orm.ConflictKey(a.Key())).DoNothing().Where(orm.Eq(a.First, "condition")),
		orm.OnConflict(orm.ConflictKey(a.Key())).DoUpdate(orm.ConflictIncoming(a.First)).Where(orm.Eq(b.First, "other")),
	}
	begun := 0
	var sqls []string
	db := testDB(t, testConnector{onBegin: func() { begun++ }, statements: &sqls})
	for i, p := range policies {
		_, err := orm.Upsert(t.Context(), db, a.Query(), p, orm.Set(a.Key(), 1), orm.Set(a.First, "a"), orm.Set(a.Last, "b"))
		if err == nil || called != 0 || begun != 0 || len(sqls) != 0 {
			t.Fatalf("policy %d: err=%v called=%d begin=%d", i, err, called, begun)
		}
	}
	p := orm.OnConflict(orm.ConflictKey(a.Key())).DoNothing()
	_, err := orm.Upsert(t.Context(), db, a.Query().Where(orm.Eq(a.First, "filter")), p, orm.Set(a.Key(), 1), orm.Set(a.First, "a"), orm.Set(a.Last, "b"))
	if err == nil || called != 0 || begun != 0 {
		t.Fatal("query predicates dropped", err)
	}
	rows := make([][]orm.Assignment[transformRow], 1001)
	if _, err := orm.UpsertMany(t.Context(), db, a.Query(), p, rows); err == nil || begun != 0 {
		t.Fatal("batch bound", err)
	}
	if _, err := orm.UpsertMany(t.Context(), db, a.Query(), p, nil); err != nil || begun != 0 {
		t.Fatal("empty batch", err)
	}
}

func TestUpsertScopesAndSkippedResults(t *testing.T) {
	var table hookedTable
	table = hookedFields(nil, orm.WithGlobalScopes(func(t hookedTable) []orm.GlobalScope[hookedRow] {
		return []orm.GlobalScope[hookedRow]{orm.Scope(t.Table, "tenant", func(ctx context.Context) (orm.Predicate[hookedRow], error) {
			return orm.Eq(t.Name, "tenant"), nil
		})}
	}))
	var sqls []string
	db := hookDB(t, testConnector{statements: &sqls, columns: hookColumns})
	assignments := []orm.Assignment[hookedRow]{orm.Set(table.Key(), 1), orm.Set(table.Body, []byte{1})}
	p := orm.OnConflict(orm.ConflictKey(table.Key())).DoUpdate(orm.ConflictIncoming(table.Name))
	got, err := orm.Upsert(t.Context(), db, table.Query(), p, assignments...)
	if !errors.Is(err, fault.Conflict) || got.IsSet() || len(sqls) != 1 || !strings.Contains(sqls[0], `"foundry_upsert"."name" =`) {
		t.Fatal("tenant update", err, sqls)
	}
	got, err = orm.Upsert(t.Context(), db, table.Query(), p.Where(orm.Eq(table.Name, "condition")), assignments...)
	if err != nil || got.IsSet() {
		t.Fatal("conditional skip", err)
	}
	got, err = orm.Upsert(t.Context(), db, table.Query(), p.DoNothing(), assignments...)
	if err != nil || got.IsSet() {
		t.Fatal("nothing skip", err)
	}
	got, err = orm.Upsert(t.Context(), db, orm.WithoutGlobalScopes(table.Query()), p, assignments...)
	if err != nil || got.IsSet() {
		t.Fatal("explicit bypass", err)
	}
}

func TestUpsertOutcomeAndRowsClosed(t *testing.T) {
	table := hookedFields(nil)
	closed := false
	db := hookDB(t, testConnector{columns: hookColumns, values: hookValues("stored", nil, nil), onClose: func() { closed = true }, commitErr: errors.New("commit failed")})
	_, err := orm.Upsert(t.Context(), db, table.Query(), orm.OnConflict[hookedRow]().DoNothing(), orm.Set(table.Key(), 1), orm.Set(table.Body, []byte{1}))
	var failure *orm.WriteError[value.Optional[hookedRow]]
	if !closed || !errors.As(err, &failure) || !failure.Candidate().IsSet() {
		t.Fatal("reconciliation", err, closed)
	}
}

func TestConflictFormattingRedactsValues(t *testing.T) {
	table := hookedFields(nil)
	u := orm.ConflictSet(table.Note, hookedInput{"private input"})
	p := orm.OnConflict(orm.ConflictKey(table.Key())).DoUpdate(u)
	for _, v := range []any{u, p} {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			if strings.Contains(fmt.Sprintf(format, v), "private input") {
				t.Fatal("leaked conflict input")
			}
		}
	}
}

func TestUpsertFinalShapeBeforeMutators(t *testing.T) {
	calls := 0
	transform := func(s string) (string, error) { calls++; return s, nil }
	table := transformFields(transform, transform, transform)
	var sqls []string
	db := testDB(t, testConnector{statements: &sqls})
	_, err := orm.Upsert(t.Context(), db, table.Query(), orm.OnConflict[transformRow]().DoNothing(), orm.Set(table.Key(), 1), orm.Set(table.First, "private"))
	if err == nil || calls != 0 || len(sqls) != 0 {
		t.Fatal("required field preflight", err, calls, sqls)
	}
}

func TestUpsertSealsProposedValuesWithGeneratedIdentity(t *testing.T) {
	table := secureRecords()
	begins := 0
	db := testDB(t, testConnector{onBegin: func() { begins++ }, respondBound: func(_ string, args []driver.NamedValue) ([]string, [][]driver.Value, error) {
		if len(args) != 3 || !strings.HasPrefix(args[1].Value.(string), "fg1:") || args[2].Value != nil {
			t.Error("unsealed proposed values")
		}
		return []string{"id", "secret", "backup"}, [][]driver.Value{{args[0].Value, args[1].Value, args[2].Value}}, nil
	}}, database.WithEncryption(sensitiveKeys(t)))
	got, err := orm.Upsert(t.Context(), db, table.Query(), orm.OnConflict(orm.ConflictKey(table.Key())).DoNothing(), orm.Set(table.Secret, encrypted.NewText("private proposed value")), orm.SetNull(table.Backup))
	if err != nil {
		t.Fatal(err)
	}
	stored, ok := got.Get()
	if !ok || stored.ID.IsZero() || stored.Secret.Reveal() != "private proposed value" || begins != 1 {
		t.Fatal("upsert sealing or hydration failed")
	}
}

// Every assignment family must remain available through the closed capture
// constraint. Zero declarations must still fail safely without invoking a codec.
func TestConflictCaptureFamilies(t *testing.T) {
	updates := []orm.ConflictUpdate[transformRow]{
		orm.ConflictSet(orm.Column[transformRow, bool]{}, false),
		orm.ConflictSet(orm.NullableColumn[transformRow, bool]{}, false),
		orm.ConflictNull(orm.NullableColumn[transformRow, bool]{}),
		orm.ConflictSet(orm.Key[transformRow, int64]{}, int64(1)),
		orm.ConflictSet(orm.Number[transformRow, int]{}, 1),
		orm.ConflictSet(orm.NullableNumber[transformRow, int]{}, 1),
		orm.ConflictNull(orm.NullableNumber[transformRow, int]{}),
		orm.ConflictSet(orm.Text[transformRow, keyStatus]{}, "name"),
		orm.ConflictSet(orm.NullableText[transformRow, keyStatus]{}, "name"),
		orm.ConflictNull(orm.NullableText[transformRow, keyStatus]{}),
		orm.ConflictSet(orm.Time[transformRow, time.Time]{}, time.Time{}),
		orm.ConflictSet(orm.NullableTime[transformRow, time.Time]{}, time.Time{}),
		orm.ConflictNull(orm.NullableTime[transformRow, time.Time]{}),
		orm.ConflictSet(orm.Enum[transformRow, keyStatus]{}, keyStatus("name")),
		orm.ConflictSet(orm.NullableEnum[transformRow, keyStatus]{}, keyStatus("name")),
		orm.ConflictNull(orm.NullableEnum[transformRow, keyStatus]{}),
		orm.ConflictSet(orm.JSON[transformRow, string]{}, value.JSON[string]{}),
		orm.ConflictSet(orm.NullableJSON[transformRow, string]{}, value.JSON[string]{}),
		orm.ConflictNull(orm.NullableJSON[transformRow, string]{}),
		orm.ConflictSet(orm.Interval[transformRow]{}, temporal.Interval{}),
		orm.ConflictSet(orm.NullableInterval[transformRow]{}, temporal.Interval{}),
		orm.ConflictNull(orm.NullableInterval[transformRow]{}),
		orm.ConflictSet(orm.InputColumn[transformRow, string, bool]{}, "fresh"),
		orm.ConflictSet(orm.NullableInputColumn[transformRow, string, bool]{}, "fresh"),
		orm.ConflictNull(orm.NullableInputColumn[transformRow, string, bool]{}),
		orm.ConflictSet(orm.InputText[transformRow, string, keyStatus]{}, "fresh"),
		orm.ConflictSet(orm.NullableInputText[transformRow, string, keyStatus]{}, "fresh"),
		orm.ConflictNull(orm.NullableInputText[transformRow, string, keyStatus]{}),
		orm.ConflictSet(orm.InputNumber[transformRow, string, int64]{}, "fresh"),
		orm.ConflictSet(orm.NullableInputNumber[transformRow, string, int64]{}, "fresh"),
		orm.ConflictNull(orm.NullableInputNumber[transformRow, string, int64]{}),
		orm.ConflictSet(orm.InputTime[transformRow, string, time.Time]{}, "fresh"),
		orm.ConflictSet(orm.NullableInputTime[transformRow, string, time.Time]{}, "fresh"),
		orm.ConflictNull(orm.NullableInputTime[transformRow, string, time.Time]{}),
		orm.ConflictSet(orm.InputEnum[transformRow, string, keyStatus]{}, "fresh"),
		orm.ConflictSet(orm.NullableInputEnum[transformRow, string, keyStatus]{}, "fresh"),
		orm.ConflictNull(orm.NullableInputEnum[transformRow, string, keyStatus]{}),
		orm.ConflictSet(orm.InputJSON[transformRow, string, string]{}, "fresh"),
		orm.ConflictSet(orm.NullableInputJSON[transformRow, string, string]{}, "fresh"),
		orm.ConflictNull(orm.NullableInputJSON[transformRow, string, string]{}),
		orm.ConflictSet(orm.InputInterval[transformRow, string]{}, "fresh"),
		orm.ConflictSet(orm.NullableInputInterval[transformRow, string]{}, "fresh"),
		orm.ConflictNull(orm.NullableInputInterval[transformRow, string]{}),
		orm.ConflictSet(orm.InputKey[transformRow, string, int64]{}, "fresh"),
	}
	for i, update := range updates {
		if orm.ConflictUpdateError(update) == nil {
			t.Errorf("uninitialized family %d was accepted", i)
		}
	}
}

func TestConflictMutatorsNormalizeLiteralAssignmentsOnce(t *testing.T) {
	var calls []string
	transform := func(name string) func(string) (string, error) {
		return func(s string) (string, error) {
			calls = append(calls, name+":"+s)
			return strings.ToLower(strings.TrimSpace(s)), nil
		}
	}
	d := transformFields(transform("first"), transform("last"), transform("maybe"))
	policy := orm.OnConflict(orm.ConflictKey(d.Key())).DoUpdate(orm.ConflictSet(d.Last, " LAST "), orm.ConflictSet(d.First, " FIRST "))
	input := [][]orm.Assignment[transformRow]{{orm.Set(d.Key(), 1), orm.Set(d.First, " A "), orm.Set(d.Last, " B ")}, {orm.Set(d.Key(), 2), orm.Set(d.First, " C "), orm.Set(d.Last, " D ")}}
	var args [][]any
	db := testDB(t, testConnector{bindings: &args, columns: []string{"id", "first", "maybe", "last"}, values: [][]driver.Value{{int64(1), "first", nil, "last"}}})
	for range 2 {
		calls = nil
		if _, e := orm.UpsertMany(t.Context(), db, d.Query(), policy, input); e != nil {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(calls, []string{"first: A ", "last: B ", "first: C ", "last: D ", "first: FIRST ", "last: LAST "}) {
			t.Fatal("declaration-order policy mutation", calls)
		}
	}
	if !reflect.DeepEqual(args[0], []any{int64(1), "a", "b", int64(2), "c", "d", "first", "last"}) || !reflect.DeepEqual(args[0], args[1]) {
		t.Fatal("reusable policy inputs", args)
	}
	for _, u := range []orm.ConflictUpdate[transformRow]{orm.ConflictNull(d.Maybe), orm.ConflictIncoming(d.Maybe)} {
		calls = nil
		if _, e := orm.UpsertMany(t.Context(), db, d.Query(), policy.DoUpdate(u), input); e != nil || len(calls) != 4 {
			t.Fatal("NULL/incoming invoked mutator", calls, e)
		}
	}
}
func TestConflictMutatorsPreserveErrorsAndPrivateInputs(t *testing.T) {
	stop := errors.New("reject conflict value")
	d := transformFields(func(s string) (string, error) {
		if s == "private-input" {
			return "", stop
		}
		return s, nil
	}, func(s string) (string, error) { return s, nil }, func(s string) (string, error) { return s, nil })
	update := orm.ConflictSet(d.First, "private-input")
	policy := orm.OnConflict(orm.ConflictKey(d.Key())).DoUpdate(update)
	var sql []string
	db := testDB(t, testConnector{statements: &sql})
	if _, e := orm.Upsert(t.Context(), db, d.Query(), policy, orm.Set(d.Key(), 1), orm.Set(d.First, "input"), orm.Set(d.Last, "input")); !errors.Is(e, stop) || len(sql) != 0 {
		t.Fatal("policy-only failure lost identity or reached SQL", e, sql)
	}
	for _, v := range []any{update, policy} {
		for _, format := range []string{"%v", "%#v"} {
			if strings.Contains(fmt.Sprintf(format, v), "private-input") {
				t.Fatal("policy diagnostic disclosed input")
			}
		}
	}
}
