package query

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type pageFailureExecutor struct {
	database.Executor
	calls int
	err   error
}

func (e *pageFailureExecutor) Query(context.Context, string, ...any) (*database.Rows, error) {
	e.calls++
	return nil, e.err
}

func TestResultPageValidationAndImmutableWindows(t *testing.T) {
	base := cursorQuery()
	id := NewOrderedField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	q := SelectRecord(base, base.Scope()).OrderBy(id.Asc())
	original, err := q.Compile()
	if err != nil {
		t.Fatal(err)
	}
	for _, lookahead := range []bool{false, true} {
		window, err := q.reader().pageWindow(PageRequest{Number: 3, Size: 2}, lookahead)
		if err != nil {
			t.Fatal(err)
		}
		s, err := window.Compile()
		limit := int64(2)
		if lookahead {
			limit++
		}
		if err != nil || !reflect.DeepEqual(s.Arguments(), []any{limit, int64(4)}) {
			t.Fatal("incorrect page window", s, err)
		}
	}
	unchanged, _ := q.Compile()
	if original.SQL() != unchanged.SQL() || !reflect.DeepEqual(original.Arguments(), unchanged.Arguments()) {
		t.Fatal("pagination mutated base")
	}
	for _, bad := range []ProjectionQuery[cursorRecord, cursorRecord]{q.Limit(0), q.Offset(1), q.Offset(-1), SelectRecord(base, base.Scope()), {}, q.OrderBy(Order[cursorRecord]{field: fieldRef{"wrong", "id"}})} {
		executor := &pageFailureExecutor{err: errors.New("unexpected executor access")}
		if p, err := bad.Paginate(t.Context(), executor, PageRequest{1, 2}); err == nil || p.Items != nil || p.Total != 0 {
			t.Fatal("invalid numbered page returned", err)
		}
		if p, err := bad.SimplePaginate(t.Context(), executor, PageRequest{1, 2}); err == nil || p.Items != nil || p.HasMore {
			t.Fatal("invalid simple page returned", err)
		}
		if executor.calls != 0 {
			t.Fatal("invalid declaration reached database")
		}
	}
	for _, request := range []PageRequest{{}, {0, 2}, {1, MaxPageSize + 1}, {math.MaxInt, 2}} {
		e := &pageFailureExecutor{err: errors.New("unexpected request")}
		if _, err := q.SimplePaginate(t.Context(), e, request); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		if _, err := q.Paginate(t.Context(), e, request); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		if _, err := base.SimplePaginate(t.Context(), e, request); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
		if e.calls != 0 {
			t.Fatal("invalid request reached executor")
		}
	}
}

func TestPageFailuresAndLookaheadOwnership(t *testing.T) {
	stop := errors.New("read failed")
	for _, total := range []int64{-1, 0, math.MaxInt64} {
		read := false
		page, err := readPage(PageRequest{1, 2}, func() (int64, error) { return total, nil }, func() ([]int, error) { read = true; return []int{1, 2}, nil })
		if total < 0 {
			if !errors.Is(err, fault.Invalid) || read || page.Items != nil {
				t.Fatal("negative count proceeded to rows")
			}
		} else if err != nil || page.Pages != total/2+total%2 || page.Total != total {
			t.Fatal("page count overflow", err)
		}
	}
	page, err := readPage(PageRequest{1, 2}, func() (int64, error) { return 5, nil }, func() ([]int, error) { return []int{1}, stop })
	if !errors.Is(err, stop) || !reflect.DeepEqual(page, Page[int]{}) {
		t.Fatal("partial page escaped")
	}
	page, err = readPage(PageRequest{1, 2}, func() (int64, error) { return 0, stop }, func() ([]int, error) { t.Fatal("read followed count failure"); return nil, nil })
	if !errors.Is(err, stop) || page.Items != nil {
		t.Fatal("count error lost")
	}
	backing := []*int{new(int), new(int), new(int)}
	items, more := trimPageLookahead(backing, 2)
	if !more || len(items) != 2 || cap(items) != 2 || backing[2] != nil {
		t.Fatal("lookahead retained hidden references")
	}
	q := cursorQuery()
	id := NewOrderedField[cursorRecord, int64]("records", "id", codec.Signed[int64]())
	r := SelectRecord(q, q.Scope()).OrderBy(id.Asc())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, ctx := range []context.Context{nil, ctx, t.Context()} {
		e := &pageFailureExecutor{err: stop}
		if p, err := r.Paginate(ctx, e, PageRequest{1, 2}); err == nil || p.Items != nil {
			t.Fatal("failed page exposed results")
		}
		if p, err := r.SimplePaginate(ctx, e, PageRequest{1, 2}); err == nil || p.Items != nil {
			t.Fatal("failed simple page exposed results")
		}
		if p, err := q.SimplePaginate(ctx, e, PageRequest{1, 2}); err == nil || p.Items != nil {
			t.Fatal("failed model page exposed results")
		}
		if ctx == nil || ctx.Err() != nil {
			if e.calls != 0 {
				t.Fatal("invalid context reached executor")
			}
		} else if e.calls != 3 {
			t.Fatal("failure did not stop read", e.calls)
		}
	}
}
