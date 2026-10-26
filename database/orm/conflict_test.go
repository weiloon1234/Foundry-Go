package orm

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
	policy := OnConflict(ConflictKey(fields.Key())).DoUpdate(ConflictIncoming(fields.First), ConflictSet(fields.Maybe, " literal "), ConflictNull(fields.Maybe))
	// Duplicate targets are rejected before any callback or SQL.
	begun := 0
	var sqls []string
	var binds [][]any
	closed := 0
	db := testDB(t, testConnector{onBegin: func() { begun++ }, statements: &sqls, bindings: &binds, onClose: func() { closed++ }, columns: []string{"id", "first", "maybe", "last"}, values: [][]driver.Value{{int64(1), "stored", nil, "last"}}})
	rows := [][]Assignment[transformRow]{{fields.Key().Set(1), fields.First.Set(" a "), fields.Last.Set(" b ")}, {fields.Key().Set(2), fields.First.Set(" c "), fields.Last.Set(" d ")}}
	if _, err := UpsertMany(t.Context(), db, fields.Query(), policy, rows); err == nil || begun != 0 || len(calls) != 0 {
		t.Fatal("duplicate preflight", err, begun, calls)
	}
	policy = OnConflict(ConflictKey(fields.Key())).DoUpdate(ConflictIncoming(fields.First), ConflictSet(fields.Maybe, " literal "))
	for range 2 {
		calls = nil
		got, err := UpsertMany(t.Context(), db, fields.Query(), policy, rows)
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
	_, err := Upsert(t.Context(), db, fields.Query(), policy.DoUpdate(ConflictNull(fields.Maybe)), rows[0]...)
	if err != nil || !reflect.DeepEqual(calls, []string{"first: a ", "last: b "}) {
		t.Fatal("NULL invoked mutator", calls, err)
	}
}

func TestUpsertSkipsLifecycleButKeepsConventionsAndDistinctInputs(t *testing.T) {
	called := 0
	table := hookedFields(func() Hooks[hookedRow] { called++; return Hooks[hookedRow]{} })
	observer, err := NewObserver[hookedRow]("upsert").Declare(func() Hooks[hookedRow] { called++; return Hooks[hookedRow]{} })
	if err != nil {
		t.Fatal(err)
	}
	var sqls []string
	var binds [][]any
	db := hookDB(t, testConnector{statements: &sqls, bindings: &binds, columns: hookColumns, values: hookValues("stored", "note", nil)}, observer)
	policy := OnConflict(ConflictKey(table.Key())).DoUpdate(ConflictSet(table.Note, hookedInput{" policy "}), ConflictSet(table.Updated, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)))
	got, err := Upsert(t.Context(), db, table.Query(), policy, table.Key().Set(1), table.Name.Set(" proposed "), table.Body.Set([]byte{1}), table.Note.Set(hookedInput{" input "}))
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
	policies := []Conflict[transformRow]{
		{}, OnConflict[transformRow]().DoUpdate(ConflictIncoming(a.First)),
		OnConflictConstraint[transformRow]("not a constraint").DoNothing(),
		OnConflict(ConflictKey(a.Key())).DoUpdate(ConflictIncoming(a.Key())),
		OnConflict(ConflictKey(a.Key())).DoUpdate(ConflictIncoming(b.First)),
		OnConflict(ConflictKey(a.Key())).DoNothing().Where(a.First.Eq("condition")),
		OnConflict(ConflictKey(a.Key())).DoUpdate(ConflictIncoming(a.First)).Where(b.First.Eq("other")),
	}
	begun := 0
	var sqls []string
	db := testDB(t, testConnector{onBegin: func() { begun++ }, statements: &sqls})
	for i, p := range policies {
		_, err := Upsert(t.Context(), db, a.Query(), p, a.Key().Set(1), a.First.Set("a"), a.Last.Set("b"))
		if err == nil || called != 0 || begun != 0 || len(sqls) != 0 {
			t.Fatalf("policy %d: err=%v called=%d begin=%d", i, err, called, begun)
		}
	}
	p := OnConflict(ConflictKey(a.Key())).DoNothing()
	_, err := Upsert(t.Context(), db, a.Query().Where(a.First.Eq("filter")), p, a.Key().Set(1), a.First.Set("a"), a.Last.Set("b"))
	if err == nil || called != 0 || begun != 0 {
		t.Fatal("query predicates dropped", err)
	}
	rows := make([][]Assignment[transformRow], 1001)
	if _, err := UpsertMany(t.Context(), db, a.Query(), p, rows); err == nil || begun != 0 {
		t.Fatal("batch bound", err)
	}
	if _, err := UpsertMany(t.Context(), db, a.Query(), p, nil); err != nil || begun != 0 {
		t.Fatal("empty batch", err)
	}
}

func TestUpsertScopesAndSkippedResults(t *testing.T) {
	var table hookedTable
	table = hookedFields(nil, WithGlobalScopes(func(t hookedTable) []GlobalScope[hookedRow] {
		return []GlobalScope[hookedRow]{Scope(t.Table, "tenant", func(ctx context.Context) (Predicate[hookedRow], error) { return t.Name.Eq("tenant"), nil })}
	}))
	var sqls []string
	db := hookDB(t, testConnector{statements: &sqls, columns: hookColumns})
	assignments := []Assignment[hookedRow]{table.Key().Set(1), table.Body.Set([]byte{1})}
	p := OnConflict(ConflictKey(table.Key())).DoUpdate(ConflictIncoming(table.Name))
	got, err := Upsert(t.Context(), db, table.Query(), p, assignments...)
	if !errors.Is(err, fault.Conflict) || got.IsSet() || len(sqls) != 1 || !strings.Contains(sqls[0], `"foundry_upsert"."name" =`) {
		t.Fatal("tenant update", err, sqls)
	}
	got, err = Upsert(t.Context(), db, table.Query(), p.Where(table.Name.Eq("condition")), assignments...)
	if err != nil || got.IsSet() {
		t.Fatal("conditional skip", err)
	}
	got, err = Upsert(t.Context(), db, table.Query(), p.DoNothing(), assignments...)
	if err != nil || got.IsSet() {
		t.Fatal("nothing skip", err)
	}
	got, err = Upsert(t.Context(), db, WithoutGlobalScopes(table.Query()), p, assignments...)
	if err != nil || got.IsSet() {
		t.Fatal("explicit bypass", err)
	}
}

func TestUpsertOutcomeAndRowsClosed(t *testing.T) {
	table := hookedFields(nil)
	closed := false
	db := hookDB(t, testConnector{columns: hookColumns, values: hookValues("stored", nil, nil), onClose: func() { closed = true }, commitErr: errors.New("commit failed")})
	_, err := Upsert(t.Context(), db, table.Query(), OnConflict[hookedRow]().DoNothing(), table.Key().Set(1), table.Body.Set([]byte{1}))
	var failure *WriteError[value.Optional[hookedRow]]
	if !closed || !errors.As(err, &failure) || !failure.Candidate().IsSet() {
		t.Fatal("reconciliation", err, closed)
	}
}

func TestConflictFormattingRedactsValues(t *testing.T) {
	table := hookedFields(nil)
	u := ConflictSet(table.Note, hookedInput{"private input"})
	p := OnConflict(ConflictKey(table.Key())).DoUpdate(u)
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
	_, err := Upsert(t.Context(), db, table.Query(), OnConflict[transformRow]().DoNothing(), table.Key().Set(1), table.First.Set("private"))
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
	got, err := Upsert(t.Context(), db, table.Query(), OnConflict(ConflictKey(table.Key())).DoNothing(), table.Secret.Set(encrypted.NewText("private proposed value")), table.Backup.SetNull())
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
	updates := []ConflictUpdate[transformRow]{
		ConflictSet(Column[transformRow, bool]{}, false),
		ConflictSet(NullableColumn[transformRow, bool]{}, false),
		ConflictNull(NullableColumn[transformRow, bool]{}),
		ConflictSet(Key[transformRow, int64]{}, int64(1)),
		ConflictSet(Number[transformRow, int]{}, 1),
		ConflictSet(NullableNumber[transformRow, int]{}, 1),
		ConflictNull(NullableNumber[transformRow, int]{}),
		ConflictSet(Text[transformRow, keyStatus]{}, "name"),
		ConflictSet(NullableText[transformRow, keyStatus]{}, "name"),
		ConflictNull(NullableText[transformRow, keyStatus]{}),
		ConflictSet(Time[transformRow, time.Time]{}, time.Time{}),
		ConflictSet(NullableTime[transformRow, time.Time]{}, time.Time{}),
		ConflictNull(NullableTime[transformRow, time.Time]{}),
		ConflictSet(Enum[transformRow, keyStatus]{}, keyStatus("name")),
		ConflictSet(NullableEnum[transformRow, keyStatus]{}, keyStatus("name")),
		ConflictNull(NullableEnum[transformRow, keyStatus]{}),
		ConflictSet(JSON[transformRow, string]{}, value.JSON[string]{}),
		ConflictSet(NullableJSON[transformRow, string]{}, value.JSON[string]{}),
		ConflictNull(NullableJSON[transformRow, string]{}),
		ConflictSet(Interval[transformRow]{}, temporal.Interval{}),
		ConflictSet(NullableInterval[transformRow]{}, temporal.Interval{}),
		ConflictNull(NullableInterval[transformRow]{}),
		ConflictSet(InputColumn[transformRow, string, bool]{}, "fresh"),
		ConflictSet(NullableInputColumn[transformRow, string, bool]{}, "fresh"),
		ConflictNull(NullableInputColumn[transformRow, string, bool]{}),
		ConflictSet(InputText[transformRow, string, keyStatus]{}, "fresh"),
		ConflictSet(NullableInputText[transformRow, string, keyStatus]{}, "fresh"),
		ConflictNull(NullableInputText[transformRow, string, keyStatus]{}),
		ConflictSet(InputNumber[transformRow, string, int64]{}, "fresh"),
		ConflictSet(NullableInputNumber[transformRow, string, int64]{}, "fresh"),
		ConflictNull(NullableInputNumber[transformRow, string, int64]{}),
		ConflictSet(InputTime[transformRow, string, time.Time]{}, "fresh"),
		ConflictSet(NullableInputTime[transformRow, string, time.Time]{}, "fresh"),
		ConflictNull(NullableInputTime[transformRow, string, time.Time]{}),
		ConflictSet(InputEnum[transformRow, string, keyStatus]{}, "fresh"),
		ConflictSet(NullableInputEnum[transformRow, string, keyStatus]{}, "fresh"),
		ConflictNull(NullableInputEnum[transformRow, string, keyStatus]{}),
		ConflictSet(InputJSON[transformRow, string, string]{}, "fresh"),
		ConflictSet(NullableInputJSON[transformRow, string, string]{}, "fresh"),
		ConflictNull(NullableInputJSON[transformRow, string, string]{}),
		ConflictSet(InputInterval[transformRow, string]{}, "fresh"),
		ConflictSet(NullableInputInterval[transformRow, string]{}, "fresh"),
		ConflictNull(NullableInputInterval[transformRow, string]{}),
		ConflictSet(InputKey[transformRow, string, int64]{}, "fresh"),
	}
	for i, update := range updates {
		if update.state.err == nil {
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
	policy := OnConflict(ConflictKey(d.Key())).DoUpdate(ConflictSet(d.Last, " LAST "), ConflictSet(d.First, " FIRST "))
	input := [][]Assignment[transformRow]{{d.Key().Set(1), d.First.Set(" A "), d.Last.Set(" B ")}, {d.Key().Set(2), d.First.Set(" C "), d.Last.Set(" D ")}}
	var args [][]any
	db := testDB(t, testConnector{bindings: &args, columns: []string{"id", "first", "maybe", "last"}, values: [][]driver.Value{{int64(1), "first", nil, "last"}}})
	for range 2 {
		calls = nil
		if _, e := UpsertMany(t.Context(), db, d.Query(), policy, input); e != nil {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(calls, []string{"first: A ", "last: B ", "first: C ", "last: D ", "first: FIRST ", "last: LAST "}) {
			t.Fatal("declaration-order policy mutation", calls)
		}
	}
	if !reflect.DeepEqual(args[0], []any{int64(1), "a", "b", int64(2), "c", "d", "first", "last"}) || !reflect.DeepEqual(args[0], args[1]) {
		t.Fatal("reusable policy inputs", args)
	}
	for _, u := range []ConflictUpdate[transformRow]{ConflictNull(d.Maybe), ConflictIncoming(d.Maybe)} {
		calls = nil
		if _, e := UpsertMany(t.Context(), db, d.Query(), policy.DoUpdate(u), input); e != nil || len(calls) != 4 {
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
	update := ConflictSet(d.First, "private-input")
	policy := OnConflict(ConflictKey(d.Key())).DoUpdate(update)
	var sql []string
	db := testDB(t, testConnector{statements: &sql})
	if _, e := Upsert(t.Context(), db, d.Query(), policy, d.Key().Set(1), d.First.Set("input"), d.Last.Set("input")); !errors.Is(e, stop) || len(sql) != 0 {
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
