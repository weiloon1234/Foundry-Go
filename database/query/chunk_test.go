package query

import (
	"context"
	"database/sql/driver"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestChunkPlanPreservesWindowAndOwnsProgress(t *testing.T) {
	q := cursorQuery().Limit(3).Offset(5)
	p, err := q.chunkPlan(2, false)
	if err != nil {
		t.Fatal(err)
	}
	s, err := p.window().Compile()
	if err != nil || !reflect.DeepEqual(s.Arguments(), []any{int64(2), int64(5)}) {
		t.Fatal("wrong first chunk", s, err)
	}
	more, err := p.advance([]cursorRecord{{ID: 6}, {ID: 7}})
	if err != nil || !more || p.take() != 1 {
		t.Fatal("lost total limit", err)
	}
	s, err = p.window().Compile()
	if err != nil || !reflect.DeepEqual(s.Arguments(), []any{int64(1), int64(7)}) {
		t.Fatal("wrong second chunk", s, err)
	}
	if more, err := p.advance([]cursorRecord{{ID: 8}}); err != nil || more {
		t.Fatal("limit not exhausted", err)
	}
	if len(q.orders) != 0 || q.offset != 5 {
		t.Fatal("chunk mutated original query")
	}
	id := NewOrderedField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	rank := NewNullableOrderedField[cursorRecord, int64]("records", "rank", codec.Signed[int64]())
	keyed, err := cursorQuery().Where(rank.Gt(1)).OrderBy(id.Desc()).chunkPlan(2, true)
	if err != nil {
		t.Fatal(err)
	}
	batch := []cursorRecord{{ID: 9}, {ID: 8}}
	if more, err := keyed.advance(batch); err != nil || !more {
		t.Fatal(err)
	}
	batch[1].ID = 999
	s, err = keyed.window().Compile()
	if err != nil || !strings.Contains(s.SQL(), `"id" <`) || !reflect.DeepEqual(s.Arguments(), []any{int64(1), int64(8), int64(2)}) {
		t.Fatal("callback could redirect key boundary", s, err)
	}
	for i := 0; i < 100; i++ {
		if _, err := keyed.advance([]cursorRecord{{ID: int64(7 - i)}, {ID: int64(6 - i)}}); err != nil {
			t.Fatal(err)
		}
		if len(keyed.window().predicates) != 2 {
			t.Fatal("key predicates accumulated across chunks")
		}
	}
}

func TestChunkValidationAndProgressFailures(t *testing.T) {
	id := NewOrderedField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	rank := NewNullableOrderedField[cursorRecord, int64]("records", "rank", codec.Signed[int64]())
	base := cursorQuery()
	for _, q := range []Query[cursorRecord]{base.Offset(1), base.OrderBy(rank.Asc()), base.OrderBy(id.Asc(), id.Desc()), {}} {
		if _, err := q.chunkPlan(2, true); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid key traversal accepted", err)
		}
	}
	missing := base
	d := *base.definition
	d.modelFields = nil
	missing.definition = &d
	if _, err := missing.chunkPlan(2, true); !errors.Is(err, fault.Invalid) {
		t.Fatal("missing key getter accepted")
	}
	for _, size := range []int{-1, 0, MaxChunkSize + 1} {
		if _, err := base.chunkPlan(size, false); !errors.Is(err, fault.Invalid) {
			t.Fatal("invalid size accepted")
		}
	}
	overflow, err := base.Offset(math.MaxInt).chunkPlan(1, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := overflow.advance([]cursorRecord{{ID: 1}}); !errors.Is(err, fault.Invalid) {
		t.Fatal("offset overflowed")
	}
	p, _ := base.chunkPlan(1, true)
	if _, err := p.advance([]cursorRecord{{}, {}}); !errors.Is(err, fault.Invalid) {
		t.Fatal("oversized database chunk accepted")
	}
	stop := errors.New("key encoding failed")
	for _, key := range []ModelField[cursorRecord]{
		{get: func(cursorRecord) (driver.Value, error) { return nil, stop }},
		{get: func(cursorRecord) (driver.Value, error) { return nil, nil }},
	} {
		p.key = key
		if _, err := p.advance([]cursorRecord{{}}); err == nil {
			t.Fatal("invalid next key published")
		}
	}
	if DefaultChunkSize < 1 || DefaultChunkSize > MaxChunkSize {
		t.Fatal("invalid default chunk size")
	}
}

func TestChunkCallbacksAndContextBeforeExecution(t *testing.T) {
	stop := errors.New("unexpected read")
	executor := &pageFailureExecutor{err: stop}
	q := cursorQuery().Limit(0)
	for _, byID := range []bool{false, true} {
		if err := q.chunks(t.Context(), executor, 2, byID, func([]cursorRecord) error { t.Fatal("empty window called callback"); return nil }); err != nil {
			t.Fatal(err)
		}
		if err := q.chunks(t.Context(), executor, 2, byID, nil); !errors.Is(err, fault.Invalid) {
			t.Fatal("nil chunk callback accepted")
		}
	}
	if err := q.EachChunked(t.Context(), executor, 2, nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil row callback accepted")
	}
	if err := q.EachByID(t.Context(), executor, 2, nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil keyed callback accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, ctx := range []context.Context{nil, ctx} {
		if err := q.EachChunked(ctx, executor, 2, func(cursorRecord) error { return nil }); err == nil {
			t.Fatal("invalid context accepted")
		}
	}
	if executor.calls != 0 {
		t.Fatal("rejected or empty chunk reached database")
	}
	called := 0
	ctx, cancel = context.WithCancel(t.Context())
	err := eachBatch(ctx, func(cursorRecord) error { called++; cancel(); return nil })([]cursorRecord{{}, {}})
	if !errors.Is(err, context.Canceled) || called != 1 {
		t.Fatal("cancellation did not stop within batch")
	}
	if err := eachBatch(t.Context(), func(cursorRecord) error { return stop })([]cursorRecord{{}}); err != stop {
		t.Fatal("callback error identity changed")
	}
	// Exhausting a declared limit avoids demanding an unused next key.
	p, _ := cursorQuery().Limit(1).chunkPlan(1, true)
	p.key = ModelField[cursorRecord]{}
	if more, err := p.advance([]cursorRecord{{}}); err != nil || more || p.remaining != value.Set(0) {
		t.Fatal("limited final batch advanced")
	}
}
