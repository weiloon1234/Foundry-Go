package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestChunkPlanPreservesWindowAndOwnsProgress(t *testing.T) {
	r := records()
	q := r.Query().Limit(3).Offset(5)
	p, err := newChunkPlan(q.state, 2)
	if err != nil {
		t.Fatal(err)
	}
	first, err := compileRead(p.window(), querycore.ReadModels)
	if err != nil || !reflect.DeepEqual(first.Bindings, []any{int64(2), int64(5)}) {
		t.Fatal(first, err)
	}
	last := record{ID: 7}
	more, err := p.advance(2, unsafe.Pointer(&last))
	if err != nil || !more || p.take() != 1 {
		t.Fatal(more, err)
	}
	last.ID = 999
	second, err := compileRead(p.window(), querycore.ReadModels)
	if err != nil || strings.Contains(second.SQLText, "OFFSET") || !strings.Contains(second.SQLText, `"records"."id" > $1`) || !reflect.DeepEqual(second.Bindings, []any{int64(7), int64(1)}) {
		t.Fatal(second, err)
	}
	if more, err := p.advance(1, nil); err != nil || more {
		t.Fatal("limited last batch demanded unused key", err)
	}
	if q.state.ast.Offset != 5 || len(q.state.ast.Orders) != 0 {
		t.Fatal("original query changed")
	}
	keyed, err := newChunkPlan(r.Query().Where(r.Name.Ne("hidden")).OrderBy(r.ID.Desc()).state, 2)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		last.ID = int64(200 - i)
		if _, err := keyed.advance(2, unsafe.Pointer(&last)); err != nil {
			t.Fatal(err)
		}
		if len(keyed.window().ast.Predicates) != 2 {
			t.Fatal("continuation predicates accumulated")
		}
	}
	statement, err := compileRead(keyed.window(), querycore.ReadModels)
	if err != nil || !strings.Contains(statement.SQLText, `"id" <`) || !reflect.DeepEqual(statement.Bindings, []any{"hidden", int64(101), int64(2)}) {
		t.Fatal(statement, err)
	}
}

func TestChunkNullAndMixedOrdering(t *testing.T) {
	r := records()
	cases := []struct {
		name      string
		order     Order[record]
		note      value.Nullable[string]
		fragments []string
		values    []any
	}{
		{"ascending present", r.Note.Asc(), value.Of("m"), []string{`"note" >`, `"note" IS NULL`, `"id" >`}, []any{"m", "m", int64(7), int64(1)}},
		{"ascending null", r.Note.Asc(), value.Null[string](), []string{`"note" IS NULL`, `"id" >`}, []any{int64(7), int64(1)}},
		{"descending null", r.Note.Desc(), value.Null[string](), []string{`"note" IS NOT NULL`, `"note" IS NULL`, `"id" >`}, []any{int64(7), int64(1)}},
		{"descending present", r.Note.Desc(), value.Of("m"), []string{`"note" <`, `"note" =`, `"id" >`}, []any{"m", "m", int64(7), int64(1)}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			p, err := newChunkPlan(r.Query().OrderBy(test.order).state, 1)
			if err != nil {
				t.Fatal(err)
			}
			last := record{ID: 7, Note: test.note}
			if more, err := p.advance(1, unsafe.Pointer(&last)); err != nil || !more {
				t.Fatal(err)
			}
			s, err := compileRead(p.window(), querycore.ReadModels)
			if err != nil {
				t.Fatal(err)
			}
			for _, part := range test.fragments {
				if !strings.Contains(s.SQLText, part) {
					t.Fatalf("missing %s: %s", part, s.SQLText)
				}
			}
			if !reflect.DeepEqual(s.Bindings, test.values) {
				t.Fatalf("bindings %#v want %#v", s.Bindings, test.values)
			}
		})
	}
	p, err := newChunkPlan(r.Query().OrderBy(r.Name.Asc()).state, 1)
	if err != nil {
		t.Fatal(err)
	}
	last := record{ID: 7, Name: "m"}
	if _, err := p.advance(1, unsafe.Pointer(&last)); err != nil {
		t.Fatal(err)
	}
	s, err := compileRead(p.window(), querycore.ReadModels)
	if err != nil || !strings.Contains(s.SQLText, `("records"."display_name", "records"."id") >`) {
		t.Fatal("row-comparison fast path", s, err)
	}
}

func TestChunkFallbackAndProgressFailures(t *testing.T) {
	r := records()
	p, err := newChunkPlan(r.Query().OrderBy(r.Note.Asc().NullsFirst()).Offset(3).state, 2)
	if err != nil {
		t.Fatal(err)
	}
	if p.keys != nil {
		t.Fatal("explicit NULL placement used keyset")
	}
	if more, err := p.advance(2, nil); err != nil || !more {
		t.Fatal(err)
	}
	statement, err := compileRead(p.window(), querycore.ReadModels)
	if err != nil || !reflect.DeepEqual(statement.Bindings, []any{int64(2), int64(5)}) {
		t.Fatal(statement, err)
	}
	p.offset = math.MaxInt
	if _, err := p.advance(2, nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("offset overflow accepted", err)
	}
	p, err = newChunkPlan(r.Query().state, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.advance(2, nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("oversized batch accepted", err)
	}
	stop := errors.New("key bind failed")
	for _, getter := range []func(unsafe.Pointer) (driver.Value, error){func(unsafe.Pointer) (driver.Value, error) { return nil, stop }, func(unsafe.Pointer) (driver.Value, error) { return nil, nil }} {
		column := *r.ID.key.core
		column.get = getter
		p.keys = []*columnCore{&column}
		if _, err := p.advance(1, nil); err == nil {
			t.Fatal("invalid continuation key accepted")
		}
	}
}

func TestChunkReadsCloseBeforeCallbacksAndCaptureKeys(t *testing.T) {
	r := records()
	closed := false
	calls := 0
	var statements []string
	var bindings [][]any
	db := testDB(t, testConnector{statements: &statements, bindings: &bindings, onClose: func() { closed = true }, respond: func(string) ([]string, [][]driver.Value) {
		closed = false
		calls++
		first := int64(calls*2 - 1)
		rows := [][]driver.Value{{first, "row", nil, "1"}, {first + 1, "row", nil, "1"}}
		if calls == 3 {
			rows = rows[:1]
		}
		return []string{"id", "display_name", "note", "amount"}, rows
	}})
	var delivered []int64
	err := r.Query().Limit(5).Offset(3).Chunk(t.Context(), db, 2, func(items []record) error {
		if !closed {
			t.Error("callback ran before rows closed")
		}
		for _, item := range items {
			delivered = append(delivered, item.ID)
		}
		items[len(items)-1].ID = 999
		return nil
	})
	if err != nil || !reflect.DeepEqual(delivered, []int64{1, 2, 3, 4, 5}) || calls != 3 {
		t.Fatal(delivered, calls, err)
	}
	want := [][]any{{int64(2), int64(3)}, {int64(2), int64(2)}, {int64(4), int64(1)}}
	if !reflect.DeepEqual(bindings, want) {
		t.Fatalf("continuation did not own keys: %#v", bindings)
	}
	if strings.Contains(statements[1], "OFFSET") || strings.Contains(statements[2], "OFFSET") {
		t.Fatal("keyset fell back to OFFSET")
	}
}

func TestChunkValidationAndCallbackFailures(t *testing.T) {
	r := records()
	var statements []string
	db := testDB(t, testConnector{statements: &statements, columns: []string{"id", "display_name", "note", "amount"}, values: [][]driver.Value{{int64(1), "one", nil, "1"}, {int64(2), "two", nil, "2"}}})
	callback := func([]record) error { t.Error("unexpected callback"); return nil }
	for _, size := range []int{-1, 0, MaxChunkSize + 1} {
		if err := r.Query().Chunk(t.Context(), db, size, callback); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	if err := r.Query().Chunk(t.Context(), db, 1, nil); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if err := r.Query().EachChunked(t.Context(), db, 1, nil); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if err := r.Query().Limit(0).Chunk(t.Context(), db, 1, callback); err != nil {
		t.Fatal(err)
	}
	if err := r.Query().Limit(0).Where(Predicate[record]{}).Chunk(t.Context(), db, 1, callback); err == nil {
		t.Fatal("empty window skipped validation")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := r.Query().Chunk(ctx, db, 1, callback); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(statements) != 0 {
		t.Fatal("invalid requests sent SQL", statements)
	}
	stop := errors.New("callback stopped")
	if err := r.Query().Chunk(t.Context(), db, 2, func([]record) error { return stop }); err != stop {
		t.Fatal(err)
	}
	called := 0
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	err := r.Query().EachChunked(ctx, db, 2, func(record) error { called++; cancel(); return nil })
	if !errors.Is(err, context.Canceled) || called != 1 {
		t.Fatal(called, err)
	}
	if err := r.Query().Chunk(t.Context(), db, 1, callback); !errors.Is(err, fault.Invalid) {
		t.Fatal("overfull batch delivered", err)
	}
}

func TestChunkReadFailuresDoNotDeliverPartialBatches(t *testing.T) {
	r := records()
	for _, test := range []struct {
		name, amount string
		closeErr     error
	}{
		{"decode", "invalid", nil}, {"close", "2", errors.New("close failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			closed := false
			db := testDB(t, testConnector{onClose: func() { closed = true }, closeErr: test.closeErr, columns: []string{"id", "display_name", "note", "amount"}, values: [][]driver.Value{{int64(1), "one", nil, "1"}, {int64(2), "two", nil, test.amount}}})
			err := r.Query().Chunk(t.Context(), db, 2, func([]record) error { t.Fatal("partial batch delivered"); return nil })
			if err == nil || !closed {
				t.Fatal(closed, err)
			}
		})
	}
}
