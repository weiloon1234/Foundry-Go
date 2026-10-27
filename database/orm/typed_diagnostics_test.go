package orm

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

type privateTypedQueryDiagnostic struct {
	exit  bool
	calls atomic.Int32
}

func (e *privateTypedQueryDiagnostic) Error() string {
	e.calls.Add(1)
	if e.exit {
		runtime.Goexit()
	}
	panic("private typed query diagnostic")
}

func (e *privateTypedQueryDiagnostic) Format(fmt.State, rune) { _ = e.Error() }
func (*privateTypedQueryDiagnostic) Unwrap() error            { return fault.Conflict }

func TestCursorInputDiagnosticSurvivesContainment(t *testing.T) {
	for _, exit := range []bool{false, true} {
		t.Run(fmt.Sprintf("goexit=%t", exit), func(t *testing.T) {
			cause := &privateTypedQueryDiagnostic{exit: exit}
			original := querycore.CursorInputFailure(cause)
			result := callback.Isolated("cursor boundary", func() error { return original })
			var input *querycore.CursorInputError
			var retained *privateTypedQueryDiagnostic
			if !errors.Is(result, original) || !errors.Is(result, cause) || !errors.Is(result, fault.Invalid) || !errors.As(result, &input) || !errors.Is(result, input) || !errors.As(result, &retained) || retained != cause {
				t.Fatal("cursor input lost typed classification or identity")
			}
			for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
				if got := fmt.Sprintf(format, result); got != "cursor boundary: invalid cursor input or query scope" {
					t.Fatalf("cursor input diagnostic = %q", got)
				}
			}
			if cause.calls.Load() != 0 {
				t.Fatal("cursor input formatted an application cause")
			}
		})
	}
}

func TestQueryOwnedDatabaseDiagnosticsSurviveContainment(t *testing.T) {
	for _, test := range []struct {
		operation string
		code      database.Code
		message   string
	}{
		{"query first", database.NotFound, "query operation: database query first: not_found"},
		{"relation target key", database.TooManyRows, "query operation: database relation target key: too_many_rows"},
	} {
		t.Run(test.operation, func(t *testing.T) {
			original := querycore.DatabaseFailure(test.operation, test.code)
			result := callback.Isolated("query operation", func() error { return original })
			var detail *database.Error
			if !errors.Is(result, original) || !errors.Is(result, test.code) || !errors.As(result, &detail) || !errors.Is(result, detail) || detail.Code() != test.code {
				t.Fatal("query-owned database error lost classification or identity")
			}
			replacement := database.NewError("private replacement", test.code)
			var rawReplacement *database.Error
			if !errors.As(replacement, &rawReplacement) {
				t.Fatal("replacement lost its native database error")
			}
			*detail = *rawReplacement
			for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
				if got := fmt.Sprintf(format, result); got != test.message {
					t.Fatalf("query-owned database diagnostic = %q, want %q", got, test.message)
				}
			}
		})
	}
}

func TestWriteReconciliationDiagnosticRetainsCandidateAndCauses(t *testing.T) {
	type record struct {
		ID      int
		Private string
	}
	candidate := record{ID: 7, Private: "private candidate"}
	for _, exit := range []bool{false, true} {
		for _, marked := range []bool{false, true} {
			t.Run(fmt.Sprintf("marked=%t/goexit=%t", marked, exit), func(t *testing.T) {
				cause := &privateTypedQueryDiagnostic{exit: exit}
				var input error = cause
				want := "model write: model write requires transaction outcome reconciliation"
				innerWant := "database model write: commit_unknown"
				if marked {
					owned := querycore.DatabaseFailure("query first", database.NotFound)
					input = safeerror.Retain(owned, errors.Join(owned, cause))
					innerWant = "database query first: not_found"
				}
				returned, reconcile := inspectOutcome(input, true)
				if !reconcile {
					t.Fatal("unconfirmed statement did not require reconciliation")
				}
				writer := diagnosticTransactor(func(_ context.Context, work func(*database.Tx) error) error {
					if err := work(&database.Tx{}); err != nil {
						return err
					}
					return returned
				})
				var original error
				result := callback.Isolated("model write", func() error {
					got, err := atomicModels(t.Context(), writer, func(*database.Tx) (record, error) { return candidate, nil })
					if got != (record{}) {
						t.Error("uncertain write published a successful model")
					}
					original = err
					return err
				})
				var detail *WriteError[record]
				var databaseDetail *database.Error
				var retained *privateTypedQueryDiagnostic
				if !errors.Is(result, original) || !errors.Is(result, returned) || !errors.Is(result, cause) || !errors.Is(result, database.CommitUnknown) || !errors.Is(result, fault.Conflict) || !errors.As(result, &detail) || !errors.Is(result, detail) || detail.Candidate() != candidate || !errors.As(result, &databaseDetail) || databaseDetail.Outcome() != database.Unknown || !errors.As(result, &retained) || retained != cause {
					t.Fatal("write reconciliation lost candidate, outcome or cause identity")
				}
				if message, owned := safeerror.OwnedMessage(detail.Unwrap()); !owned || message != innerWant {
					t.Fatal("write reconciliation lost its retained database diagnostic")
				}
				for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
					if got := fmt.Sprintf(format, result); got != want {
						t.Fatalf("write reconciliation diagnostic = %q, want %q", got, want)
					}
				}
				if cause.calls.Load() != 0 {
					t.Fatal("write reconciliation formatted an application cause")
				}
			})
		}
	}
}

type diagnosticTransactor func(context.Context, func(*database.Tx) error) error

func (f diagnosticTransactor) Transaction(ctx context.Context, work func(*database.Tx) error, _ ...database.TxOptions) error {
	return f(ctx, work)
}
