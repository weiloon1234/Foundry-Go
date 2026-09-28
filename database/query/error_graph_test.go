package query

import (
	"context"
	"database/sql/driver"
	"errors"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"github.com/weiloon1234/Foundry-Go/value"
)

type cyclicQueryError struct{ visits atomic.Int32 }

func (*cyclicQueryError) Error() string { panic("private query failure must not be formatted") }
func (e *cyclicQueryError) Unwrap() error {
	if e.visits.Add(1) > 4096 {
		return nil
	}
	return e
}

type abnormalOutcomeError string

func (abnormalOutcomeError) Error() string { panic("private outcome failure must not be formatted") }
func (e abnormalOutcomeError) As(target any) bool {
	switch e {
	case "panic":
		panic("private outcome")
	case "goexit":
		runtime.Goexit()
	case "nil":
		if p, ok := target.(**database.Error); ok {
			*p = nil
			return true
		}
	}
	return false
}

func TestCustomTransactorErrorInspectionRetainsOnlyReconciliationCandidate(t *testing.T) {
	type record struct {
		ID      int64
		Private string
	}
	candidate := record{ID: 7, Private: "private candidate"}
	for _, mode := range []string{"cycle", "panic", "goexit", "nil", "ordinary", "unspecified", "healthy"} {
		t.Run(mode, func(t *testing.T) {
			cycle := new(cyclicQueryError)
			var failure error
			switch mode {
			case "cycle":
				failure = cycle
			case "panic", "goexit", "nil":
				failure = abnormalOutcomeError(mode)
			case "ordinary":
				failure = errors.New("ordinary failure")
			case "unspecified":
				failure = &database.Error{}
			}
			begins, writes := 0, 0
			writer := mutatorTransactor(func(_ context.Context, work func(*database.Tx) error) error {
				begins++
				if err := work(&database.Tx{}); err != nil {
					return err
				}
				return failure
			})
			write := func(context.Context, *database.Tx) (record, error) { writes++; return candidate, nil }
			got, err := executeWrite(t.Context(), writer, write)
			if begins != 1 || writes != 1 {
				t.Fatal("write retried or skipped")
			}
			if mode == "healthy" {
				if err != nil || got != candidate {
					t.Fatal("healthy write lost typed result")
				}
				return
			}
			if err == nil || got != (record{}) {
				t.Fatal("failed write published success")
			}
			detail, retained := err.(*WriteError[record])
			want := mode != "ordinary" && mode != "unspecified"
			if retained != want {
				t.Fatal("unexpected reconciliation classification")
			}
			if retained && (detail.Candidate() != candidate || strings.Contains(detail.Error(), candidate.Private)) {
				t.Fatal("candidate lost or disclosed")
			}
			if mode == "cycle" && (cycle.visits.Load() == 0 || cycle.visits.Load() > 256) {
				t.Fatal("cyclic outcome inspection exceeded its bound")
			}
			failure = nil
			got, err = executeWrite(t.Context(), writer, write)
			if err != nil || got != candidate || begins != 2 || writes != 2 {
				t.Fatal("later write did not complete")
			}
		})
	}
}

func TestCyclicCursorCodecErrorRemainsServerFailure(t *testing.T) {
	first, err := cursorQuery().cursorPlan(CursorRequest[cursorRecord]{Size: 2})
	if err != nil {
		t.Fatal(err)
	}
	token, err := makeCursor(first.scope, first.fields, cursorRecord{ID: 7})
	if err != nil {
		t.Fatal(err)
	}
	request := CursorRequest[cursorRecord]{Size: 1, After: value.Set(token)}
	for _, invalid := range []bool{false, true} {
		cycle := new(cyclicQueryError)
		var failure error = cycle
		if invalid {
			failure = errors.Join(fault.Invalid, cycle)
		}
		c := codec.New[int64](func(v int64) (driver.Value, error) { return v, nil }, func(raw any) (int64, error) {
			if failure != nil {
				return 0, failure
			}
			return codec.Signed[int64]().Decode(raw)
		})
		field := NewRecordField("id", c, func(r cursorRecord) int64 { return r.ID })
		boundary, err := readCursorBoundary(request, first.scope, []RecordField[cursorRecord]{field}, []bool{true})
		if err == nil || len(boundary.keys) != 0 {
			t.Fatal("failed codec published a cursor boundary")
		}
		visits := cycle.visits.Load()
		_, input, _ := errorgraph.As[*CursorInputError](err)
		if input != invalid || visits > 256 || !invalid && visits == 0 {
			t.Fatal("cyclic cursor classification lost bounds or a reached input marker")
		}
		failure = nil
		boundary, err = readCursorBoundary(request, first.scope, []RecordField[cursorRecord]{field}, []bool{true})
		if err != nil || len(boundary.keys) != 1 || boundary.keys[0] != int64(7) {
			t.Fatal("healthy codec did not recover")
		}
	}
}
