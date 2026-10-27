package orm

import (
	"context"
	"database/sql/driver"
	"errors"
	"github.com/weiloon1234/Foundry-Go/database"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

type cyclicOutcomeError struct{ visits atomic.Int32 }

func (*cyclicOutcomeError) Error() string { panic("private query failure must not be formatted") }

func (e *cyclicOutcomeError) Unwrap() error {
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

type outcomeWriter struct {
	db      *database.DB
	failure *error
}

func (w outcomeWriter) Transaction(ctx context.Context, fn func(*database.Tx) error, opts ...database.TxOptions) error {
	if err := w.db.Transaction(ctx, fn, opts...); err != nil {
		return err
	}
	return *w.failure
}
func TestCustomTransactorErrorInspectionRetainsOnlyReconciliationCandidate(t *testing.T) {
	for _, mode := range []string{"cycle", "panic", "goexit", "nil", "ordinary", "unspecified", "healthy"} {
		t.Run(mode, func(t *testing.T) {
			cycle := new(cyclicOutcomeError)
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
			begins := 0
			var statements []string
			db := testDB(t, testConnector{columns: []string{"id", "display_name", "note", "amount"}, values: [][]driver.Value{{int64(7), "private candidate", nil, "12.5"}}, statements: &statements, onBegin: func() { begins++ }})
			r := records()
			writer := outcomeWriter{db, &failure}
			got, err := r.Update(t.Context(), writer, 7, r.Name.Set("input"))
			if begins != 1 || len(statements) != 1 {
				t.Fatal("write retried or skipped", begins, len(statements))
			}
			if mode == "healthy" {
				if err != nil || got.ID != 7 {
					t.Fatal("healthy write failed")
				}
				return
			}
			if err == nil || got.ID != 0 {
				t.Fatal("failed write published result")
			}
			var detail *WriteError[record]
			retained := errors.As(err, &detail)
			want := mode != "ordinary" && mode != "unspecified"
			if retained != want {
				t.Fatal("reconciliation classification changed")
			}
			if retained && (detail.Candidate().ID != 7 || detail.Candidate().Name != "private candidate" || strings.Contains(detail.Error(), "private candidate")) {
				t.Fatal("candidate lost or disclosed")
			}
			if mode == "cycle" && (cycle.visits.Load() == 0 || cycle.visits.Load() > 256) {
				t.Fatal("unbounded outcome traversal")
			}
			failure = nil
			got, err = r.Update(t.Context(), writer, 7, r.Name.Set("input"))
			if err != nil || got.ID != 7 || begins != 2 || len(statements) != 2 {
				t.Fatal("later write did not recover")
			}
		})
	}
}
