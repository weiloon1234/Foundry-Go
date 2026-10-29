package rows

import (
	"context"
	"database/sql/driver"
	"errors"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/ormcore"
	"math"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

type pageTransferExecutor struct {
	database.Executor
	calls  int
	before func(string) error
}

func (e *pageTransferExecutor) Query(ctx context.Context, s string, args ...any) (*database.Rows, error) {
	e.calls++
	if e.before != nil {
		if err := e.before(s); err != nil {
			return nil, err
		}
	}
	return e.Executor.Query(ctx, s, args...)
}

func TestResultPageValidationAndImmutableWindows(t *testing.T) {
	d := declareMembers()
	base := Read(FromModel(d.Query())).OrderBy(expr.Value(orm.KeyExpr(d.ID)).Asc())
	original, e := CompileRead(base)
	if e != nil {
		t.Fatal(e)
	}
	state := &driverState{}
	ex := &pageTransferExecutor{Executor: openTest(t, state), before: func(string) error { t.Fatal("invalid page executed"); return nil }}
	for _, ahead := range []bool{false, true} {
		r, e := recordPage(t.Context(), ex, base.core, orm.Page(3, 2), ahead)
		if e != nil {
			t.Fatal(e)
		}
		s, e := ormcore.CompileRecord(t.Context(), r)
		limit := int64(2)
		if ahead {
			limit++
		}
		if e != nil || !reflect.DeepEqual(s.Bindings, []any{limit, int64(4)}) {
			t.Fatal(s, e)
		}
	}
	again, e := CompileRead(base)
	if e != nil || again.SQL() != original.SQL() || !reflect.DeepEqual(again.Arguments(), original.Arguments()) {
		t.Fatal("page mutated source", again, e)
	}
	foreign := declareMembers()
	for _, bad := range []Result[member, member]{base.Limit(0), base.Offset(1), base.Offset(-1), Read(FromModel(d.Query())), {}, base.OrderBy(expr.Value(orm.KeyExpr(foreign.ID)).Asc())} {
		if p, e := bad.Paginate(t.Context(), ex, orm.Page(1, 2)); e == nil || p.Items != nil || p.Total != 0 {
			t.Fatal(p, e)
		}
		if p, e := bad.SimplePaginate(t.Context(), ex, orm.Page(1, 2)); e == nil || p.Items != nil || p.HasMore {
			t.Fatal(p, e)
		}
	}
	for _, request := range []orm.PageRequest{{}, {Number: 0, Size: 2}, {Number: 1, Size: orm.MaxPageSize + 1}, {Number: math.MaxInt, Size: 2}} {
		if _, e := base.Paginate(t.Context(), ex, request); !errors.Is(e, fault.Invalid) {
			t.Fatal(e)
		}
		if _, e := base.SimplePaginate(t.Context(), ex, request); !errors.Is(e, fault.Invalid) {
			t.Fatal(e)
		}
		if _, e := orm.SimplePaginate(t.Context(), ex, d.Query(), request); !errors.Is(e, fault.Invalid) {
			t.Fatal(e)
		}
	}
	if ex.calls != 0 {
		t.Fatal("preflight issued SQL", ex.calls)
	}
}
func TestPageFailuresAndLookaheadOwnership(t *testing.T) {
	d := declareMembers()
	q := Read(FromModel(d.Query())).OrderBy(expr.Value(orm.KeyExpr(d.ID)).Asc())
	stop := errors.New("read failed")
	for _, total := range []int64{-1, 0, math.MaxInt64} {
		state := &driverState{}
		ex := &pageTransferExecutor{Executor: openTest(t, state)}
		read := false
		ex.before = func(s string) error {
			if strings.HasPrefix(s, "SELECT COUNT(*)") {
				state.columns = []string{"count"}
				state.rows = [][]driver.Value{{total}}
			} else {
				read = true
				state.columns = []string{"id", "name", "nickname"}
				state.rows = [][]driver.Value{{int64(1), "one", nil}, {int64(2), "two", nil}}
			}
			return nil
		}
		p, e := q.Paginate(t.Context(), ex, orm.Page(1, 2))
		if total < 0 {
			if !errors.Is(e, fault.Invalid) || read || p.Items != nil {
				t.Fatal(p, e, read)
			}
		} else if e != nil || p.Pages != total/2+total%2 || p.Total != total {
			t.Fatal("overflow", p, e)
		}
	}
	for _, failCount := range []bool{false, true} {
		state := &driverState{}
		ex := &pageTransferExecutor{Executor: openTest(t, state)}
		ex.before = func(s string) error {
			if strings.HasPrefix(s, "SELECT COUNT(*)") {
				if failCount {
					return stop
				}
				state.columns = []string{"count"}
				state.rows = [][]driver.Value{{int64(5)}}
				return nil
			}
			return stop
		}
		p, e := q.Paginate(t.Context(), ex, orm.Page(1, 2))
		wantCalls := 2
		if failCount {
			wantCalls = 1
		}
		if !errors.Is(e, stop) || !reflect.DeepEqual(p, orm.PageResult[member]{}) || ex.calls != wantCalls {
			t.Fatal("partial failed page", p, e, ex.calls)
		}
	}
	state := &driverState{columns: []string{"id", "name", "nickname"}, rows: [][]driver.Value{{int64(1), "one", nil}, {int64(2), "two", nil}, {int64(3), "hidden", nil}}}
	p, e := q.SimplePaginate(t.Context(), openTest(t, state), orm.Page(1, 2))
	if e != nil || !p.HasMore || len(p.Items) != 2 || cap(p.Items) != 2 {
		t.Fatal(p, e)
	}
	// The query fetched three entries before capping the returned slice; inspect
	// the owned backing allocation to prove the hidden row's references cleared.
	backing := unsafe.Slice(unsafe.SliceData(p.Items), 3)
	if !reflect.DeepEqual(backing[2], member{}) {
		t.Fatal("lookahead retained hidden references")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, ctx := range []context.Context{nil, ctx, t.Context()} {
		ex := &pageTransferExecutor{before: func(string) error { return stop }}
		if p, e := q.Paginate(ctx, ex, orm.Page(1, 2)); e == nil || p.Items != nil {
			t.Fatal(p, e)
		}
		if p, e := q.SimplePaginate(ctx, ex, orm.Page(1, 2)); e == nil || p.Items != nil {
			t.Fatal(p, e)
		}
		if p, e := orm.SimplePaginate(ctx, ex, d.Query(), orm.Page(1, 2)); e == nil || p.Items != nil {
			t.Fatal(p, e)
		}
		if ctx == nil || ctx.Err() != nil {
			if ex.calls != 0 {
				t.Fatal(ex.calls)
			}
		} else if ex.calls != 3 {
			t.Fatal(ex.calls)
		}
	}
}
