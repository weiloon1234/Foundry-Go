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
	policy := OnConflict(ConflictKey(KeyExpr(fields.Key()))).DoUpdate(ConflictIncoming(InputTextExpr(fields.First)), ConflictSet(NullableInputTextExpr(fields.Maybe), " literal "), ConflictNull(NullableInputTextExpr(fields.Maybe)))
	// Duplicate targets are rejected before any callback or SQL.
	begun := 0
	var sqls []string
	var binds [][]any
	closed := 0
	db := testDB(t, testConnector{onBegin: func() { begun++ }, statements: &sqls, bindings: &binds, onClose: func() { closed++ }, columns: []string{"id", "first", "maybe", "last"}, values: [][]driver.Value{{int64(1), "stored", nil, "last"}}})
	rows := [][]Assignment[transformRow]{{KeySet(fields.Key(), 1), InputTextSet(fields.First, " a "), InputTextSet(fields.Last, " b ")}, {KeySet(fields.Key(), 2), InputTextSet(fields.First, " c "), InputTextSet(fields.Last, " d ")}}
	if _, err := UpsertMany(t.Context(), db, fields.Query(), policy, rows); err == nil || begun != 0 || len(calls) != 0 {
		t.Fatal("duplicate preflight", err, begun, calls)
	}
	policy = OnConflict(ConflictKey(KeyExpr(fields.Key()))).DoUpdate(ConflictIncoming(InputTextExpr(fields.First)), ConflictSet(NullableInputTextExpr(fields.Maybe), " literal "))
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
	_, err := Upsert(t.Context(), db, fields.Query(), policy.DoUpdate(ConflictNull(NullableInputTextExpr(fields.Maybe))), rows[0]...)
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
	policy := OnConflict(ConflictKey(KeyExpr(table.Key()))).DoUpdate(ConflictSet(NullableInputTextExpr(table.Note), hookedInput{" policy "}), ConflictSet(TimeExpr(table.Updated), time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)))
	got, err := Upsert(t.Context(), db, table.Query(), policy, KeySet(table.Key(), 1), InputTextSet(table.Name, " proposed "), ColumnSet(table.Body, []byte{1}), NullableInputTextSet(table.Note, hookedInput{" input "}))
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
		{}, OnConflict[transformRow]().DoUpdate(ConflictIncoming(InputTextExpr(a.First))),
		OnConflictConstraint[transformRow]("not a constraint").DoNothing(),
		OnConflict(ConflictKey(KeyExpr(a.Key()))).DoUpdate(ConflictIncoming(KeyExpr(a.Key()))),
		OnConflict(ConflictKey(KeyExpr(a.Key()))).DoUpdate(ConflictIncoming(InputTextExpr(b.First))),
		OnConflict(ConflictKey(KeyExpr(a.Key()))).DoNothing().Where(InputTextEq(a.First, "condition")),
		OnConflict(ConflictKey(KeyExpr(a.Key()))).DoUpdate(ConflictIncoming(InputTextExpr(a.First))).Where(InputTextEq(b.First, "other")),
	}
	begun := 0
	var sqls []string
	db := testDB(t, testConnector{onBegin: func() { begun++ }, statements: &sqls})
	for i, p := range policies {
		_, err := Upsert(t.Context(), db, a.Query(), p, KeySet(a.Key(), 1), InputTextSet(a.First, "a"), InputTextSet(a.Last, "b"))
		if err == nil || called != 0 || begun != 0 || len(sqls) != 0 {
			t.Fatalf("policy %d: err=%v called=%d begin=%d", i, err, called, begun)
		}
	}
	p := OnConflict(ConflictKey(KeyExpr(a.Key()))).DoNothing()
	_, err := Upsert(t.Context(), db, a.Query().Where(InputTextEq(a.First, "filter")), p, KeySet(a.Key(), 1), InputTextSet(a.First, "a"), InputTextSet(a.Last, "b"))
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
		return []GlobalScope[hookedRow]{Scope(t.Table, "tenant", func(ctx context.Context) (Predicate[hookedRow], error) { return InputTextEq(t.Name, "tenant"), nil })}
	}))
	var sqls []string
	db := hookDB(t, testConnector{statements: &sqls, columns: hookColumns})
	assignments := []Assignment[hookedRow]{KeySet(table.Key(), 1), ColumnSet(table.Body, []byte{1})}
	p := OnConflict(ConflictKey(KeyExpr(table.Key()))).DoUpdate(ConflictIncoming(InputTextExpr(table.Name)))
	got, err := Upsert(t.Context(), db, table.Query(), p, assignments...)
	if !errors.Is(err, fault.Conflict) || got.IsSet() || len(sqls) != 1 || !strings.Contains(sqls[0], `"foundry_upsert"."name" =`) {
		t.Fatal("tenant update", err, sqls)
	}
	got, err = Upsert(t.Context(), db, table.Query(), p.Where(InputTextEq(table.Name, "condition")), assignments...)
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
	_, err := Upsert(t.Context(), db, table.Query(), OnConflict[hookedRow]().DoNothing(), KeySet(table.Key(), 1), ColumnSet(table.Body, []byte{1}))
	var failure *WriteError[value.Optional[hookedRow]]
	if !closed || !errors.As(err, &failure) || !failure.Candidate().IsSet() {
		t.Fatal("reconciliation", err, closed)
	}
}

func TestConflictFormattingRedactsValues(t *testing.T) {
	table := hookedFields(nil)
	u := ConflictSet(NullableInputTextExpr(table.Note), hookedInput{"private input"})
	p := OnConflict(ConflictKey(KeyExpr(table.Key()))).DoUpdate(u)
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
	_, err := Upsert(t.Context(), db, table.Query(), OnConflict[transformRow]().DoNothing(), KeySet(table.Key(), 1), InputTextSet(table.First, "private"))
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
	got, err := Upsert(t.Context(), db, table.Query(), OnConflict(ConflictKey(KeyExpr(table.Key()))).DoNothing(), EncryptedSet(table.Secret, encrypted.NewText("private proposed value")), NullableEncryptedSetNull(table.Backup))
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
		ConflictSet(ColumnExpr(Column[transformRow, bool]{}), false),
		ConflictSet(NullableColumnExpr(NullableColumn[transformRow, bool]{}), false),
		ConflictNull(NullableColumnExpr(NullableColumn[transformRow, bool]{})),
		ConflictSet(KeyExpr(Key[transformRow, int64]{}), int64(1)),
		ConflictSet(NumberExpr(Number[transformRow, int]{}), 1),
		ConflictSet(NullableNumberExpr(NullableNumber[transformRow, int]{}), 1),
		ConflictNull(NullableNumberExpr(NullableNumber[transformRow, int]{})),
		ConflictSet(TextExpr(Text[transformRow, keyStatus]{}), "name"),
		ConflictSet(NullableTextExpr(NullableText[transformRow, keyStatus]{}), "name"),
		ConflictNull(NullableTextExpr(NullableText[transformRow, keyStatus]{})),
		ConflictSet(TimeExpr(Time[transformRow, time.Time]{}), time.Time{}),
		ConflictSet(NullableTimeExpr(NullableTime[transformRow, time.Time]{}), time.Time{}),
		ConflictNull(NullableTimeExpr(NullableTime[transformRow, time.Time]{})),
		ConflictSet(EnumExpr(Enum[transformRow, keyStatus]{}), keyStatus("name")),
		ConflictSet(NullableEnumExpr(NullableEnum[transformRow, keyStatus]{}), keyStatus("name")),
		ConflictNull(NullableEnumExpr(NullableEnum[transformRow, keyStatus]{})),
		ConflictSet(JSONExpr(JSON[transformRow, string]{}), value.JSON[string]{}),
		ConflictSet(NullableJSONExpr(NullableJSON[transformRow, string]{}), value.JSON[string]{}),
		ConflictNull(NullableJSONExpr(NullableJSON[transformRow, string]{})),
		ConflictSet(IntervalExpr(Interval[transformRow]{}), temporal.Interval{}),
		ConflictSet(NullableIntervalExpr(NullableInterval[transformRow]{}), temporal.Interval{}),
		ConflictNull(NullableIntervalExpr(NullableInterval[transformRow]{})),
		ConflictSet(InputColumnExpr(InputColumn[transformRow, string, bool]{}), "fresh"),
		ConflictSet(NullableInputColumnExpr(NullableInputColumn[transformRow, string, bool]{}), "fresh"),
		ConflictNull(NullableInputColumnExpr(NullableInputColumn[transformRow, string, bool]{})),
		ConflictSet(InputTextExpr(InputText[transformRow, string, keyStatus]{}), "fresh"),
		ConflictSet(NullableInputTextExpr(NullableInputText[transformRow, string, keyStatus]{}), "fresh"),
		ConflictNull(NullableInputTextExpr(NullableInputText[transformRow, string, keyStatus]{})),
		ConflictSet(InputNumberExpr(InputNumber[transformRow, string, int64]{}), "fresh"),
		ConflictSet(NullableInputNumberExpr(NullableInputNumber[transformRow, string, int64]{}), "fresh"),
		ConflictNull(NullableInputNumberExpr(NullableInputNumber[transformRow, string, int64]{})),
		ConflictSet(InputTimeExpr(InputTime[transformRow, string, time.Time]{}), "fresh"),
		ConflictSet(NullableInputTimeExpr(NullableInputTime[transformRow, string, time.Time]{}), "fresh"),
		ConflictNull(NullableInputTimeExpr(NullableInputTime[transformRow, string, time.Time]{})),
		ConflictSet(InputEnumExpr(InputEnum[transformRow, string, keyStatus]{}), "fresh"),
		ConflictSet(NullableInputEnumExpr(NullableInputEnum[transformRow, string, keyStatus]{}), "fresh"),
		ConflictNull(NullableInputEnumExpr(NullableInputEnum[transformRow, string, keyStatus]{})),
		ConflictSet(InputJSONExpr(InputJSON[transformRow, string, string]{}), "fresh"),
		ConflictSet(NullableInputJSONExpr(NullableInputJSON[transformRow, string, string]{}), "fresh"),
		ConflictNull(NullableInputJSONExpr(NullableInputJSON[transformRow, string, string]{})),
		ConflictSet(InputIntervalExpr(InputInterval[transformRow, string]{}), "fresh"),
		ConflictSet(NullableInputIntervalExpr(NullableInputInterval[transformRow, string]{}), "fresh"),
		ConflictNull(NullableInputIntervalExpr(NullableInputInterval[transformRow, string]{})),
		ConflictSet(InputKeyExpr(InputKey[transformRow, string, int64]{}), "fresh"),
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
	policy := OnConflict(ConflictKey(KeyExpr(d.Key()))).DoUpdate(ConflictSet(InputTextExpr(d.Last), " LAST "), ConflictSet(InputTextExpr(d.First), " FIRST "))
	input := [][]Assignment[transformRow]{{KeySet(d.Key(), 1), InputTextSet(d.First, " A "), InputTextSet(d.Last, " B ")}, {KeySet(d.Key(), 2), InputTextSet(d.First, " C "), InputTextSet(d.Last, " D ")}}
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
	for _, u := range []ConflictUpdate[transformRow]{ConflictNull(NullableInputTextExpr(d.Maybe)), ConflictIncoming(NullableInputTextExpr(d.Maybe))} {
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
	update := ConflictSet(InputTextExpr(d.First), "private-input")
	policy := OnConflict(ConflictKey(KeyExpr(d.Key()))).DoUpdate(update)
	var sql []string
	db := testDB(t, testConnector{statements: &sql})
	if _, e := Upsert(t.Context(), db, d.Query(), policy, KeySet(d.Key(), 1), InputTextSet(d.First, "input"), InputTextSet(d.Last, "input")); !errors.Is(e, stop) || len(sql) != 0 {
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
