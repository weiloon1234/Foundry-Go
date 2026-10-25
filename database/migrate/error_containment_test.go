package migrate

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type migrationInspectionError struct {
	method, mode     string
	calls, formatted int
}

func (e *migrationInspectionError) Error() string          { e.formatted++; panic("private migration error") }
func (e *migrationInspectionError) Format(fmt.State, rune) { e.formatted++; runtime.Goexit() }
func (e *migrationInspectionError) As(any) bool {
	if e.method == "As" {
		e.inspect()
	}
	return false
}
func (e *migrationInspectionError) Is(error) bool {
	if e.method == "Is" {
		e.inspect()
	}
	return false
}
func (e *migrationInspectionError) Unwrap() error {
	if e.method != "Unwrap" {
		return nil
	}
	e.inspect()
	if e.mode == "cycle" && e.calls < 512 {
		return e
	}
	return nil
}
func (e *migrationInspectionError) inspect() {
	e.calls++
	switch e.mode {
	case "panic":
		panic("private migration inspection")
	case "goexit":
		runtime.Goexit()
	}
}

type failedMigrationExecutor struct {
	cause error
	calls int
}

func (e *failedMigrationExecutor) Exec(context.Context, string, ...any) (database.Result, error) {
	panic("unexpected migration write")
}
func (e *failedMigrationExecutor) Query(context.Context, string, ...any) (*database.Rows, error) {
	e.calls++
	return nil, e.cause
}

func TestMigrationFailureInspectionKeepsCauseAndRejectsRetryClaims(t *testing.T) {
	p := &Postgres{config: DefaultPostgresConfig()}
	for _, method := range []string{"As", "Unwrap"} {
		for _, mode := range []string{"panic", "goexit", "cycle"} {
			t.Run(method+"/"+mode, func(t *testing.T) {
				cause := &migrationInspectionError{method: method, mode: mode}
				err := p.failure(Key{Origin: "app", ID: "migration"}, cause)
				// Identity equality occurs before any hostile cause method.
				if !errors.Is(err, cause) {
					t.Fatal("original migration failure lost")
				}
				if err != cause && (strings.Contains(err.Error(), "retry") || strings.Contains(err.Error(), "not committed")) {
					t.Fatal("unclassified failure promised retry or rollback")
				}
				if cause.calls == 0 || cause.calls > 130 || cause.formatted != 0 {
					t.Fatal("unbounded or formatting inspection")
				}
			})
		}
	}
	cause := &migrationInspectionError{method: "As", mode: "panic"}
	interrupted := &InterruptedMigration{cause: cause}
	if p.failure(Key{}, interrupted) != interrupted || cause.calls != 0 {
		t.Fatal("interrupted migration precedence changed")
	}
	classified := database.NewError("migration", database.LockNotAvailable)
	if !errors.Is(p.failure(Key{}, classified), fault.Timeout) {
		t.Fatal("lock classification changed")
	}
}

func TestMigrationHistoryAndProgressDoNotMistakeHostileErrorsForMissingTables(t *testing.T) {
	p := &Postgres{config: DefaultPostgresConfig()}
	for _, progress := range []bool{false, true} {
		for _, method := range []string{"Is", "Unwrap"} {
			for _, mode := range []string{"panic", "goexit", "cycle"} {
				t.Run(fmt.Sprintf("progress=%t/%s/%s", progress, method, mode), func(t *testing.T) {
					cause := &migrationInspectionError{method: method, mode: mode}
					executor := &failedMigrationExecutor{cause: cause}
					var err error
					if progress {
						_, err = p.progress(t.Context(), executor)
					} else {
						_, err = p.history(t.Context(), executor)
					}
					if err != cause || executor.calls != 1 {
						t.Fatal("failure was swallowed or relation queries continued")
					}
					if cause.calls == 0 || cause.calls > 65 || cause.formatted != 0 {
						t.Fatal("unbounded or formatting inspection")
					}
				})
			}
		}
		executor := &failedMigrationExecutor{cause: database.NewError("lookup", database.NotFound)}
		var err error
		if progress {
			_, err = p.progress(t.Context(), executor)
		} else {
			_, err = p.history(t.Context(), executor)
		}
		if err != nil || executor.calls != 1 {
			t.Fatal("missing relation behavior changed")
		}
	}
}
