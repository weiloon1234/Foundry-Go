package database_test

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
)

type databaseClassificationError struct {
	method  string
	inspect func()
}

func TestFailedConnectionAcquisitionRetainsClassifierOwner(t *testing.T) {
	var fail atomic.Bool
	cause := errors.New("connection unavailable")
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	state := &driverState{connect: func(context.Context) error {
		if fail.Load() {
			return cause
		}
		return nil
	}}
	config := database.DefaultPoolConfig()
	config.MaxIdle = 0
	db, err := database.Open(t.Context(), database.Adapter{Connector: connector{state}, Classify: func(err error) database.Detail {
		if err == cause {
			close(entered)
			<-release
		}
		return database.Detail{Code: database.Unavailable}
	}}, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := db.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	fail.Store(true)
	done := make(chan error, 1)
	go func() { _, err := db.Exec(t.Context(), "unused"); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("acquisition classifier did not start")
	}
	wait, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	err = db.Close(wait)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) || db.Stats().Owners == 0 {
		t.Fatal("failed acquisition released owner before classifier")
	}
	release <- struct{}{}
	select {
	case err := <-done:
		if !errors.Is(err, database.Unavailable) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("acquisition did not finish")
	}
}

func TestRollbackErrorClassificationCannotEscape(t *testing.T) {
	// database/sql itself inspects driver errors with Is before returning them.
	// Its drivers must satisfy that contract; As exercises Foundry's classifier.
	state := &driverState{rollbackError: databaseClassificationError{method: "As", inspect: func() { runtime.Goexit() }}}
	db := open(t, state, nil)
	done := make(chan error, 1)
	go func() {
		done <- db.Transaction(t.Context(), func(*database.Tx) error { return errors.New("reject transaction") })
	}()
	select {
	case err := <-done:
		var detail *database.Error
		if !errors.As(err, &detail) || detail.Outcome() != database.NoCommit || db.Stats().Owners != 0 {
			t.Fatal("rollback failure lost ownership or outcome", err)
		}
	case <-time.After(time.Second):
		t.Fatal("rollback error escaped transaction")
	}
}

func (databaseClassificationError) Error() string { return "private transaction error" }
func (e databaseClassificationError) As(any) bool {
	if e.method == "As" {
		e.inspect()
	}
	return false
}
func (e databaseClassificationError) Is(error) bool {
	if e.method == "Is" {
		e.inspect()
	}
	return false
}
func (e databaseClassificationError) Unwrap() error {
	if e.method == "Unwrap" {
		e.inspect()
	}
	return nil
}

func TestTransactionErrorInspectionRetainsRollbackAndPoolOwnership(t *testing.T) {
	for _, method := range []string{"As", "Is", "Unwrap"} {
		for _, mode := range []string{"panic", "goexit", "blocked"} {
			t.Run(method+"/"+mode, func(t *testing.T) {
				state := &driverState{}
				db := open(t, state, nil)
				entered, release := make(chan struct{}, 1), make(chan struct{})
				defer close(release)
				var once sync.Once
				failure := databaseClassificationError{method: method, inspect: func() {
					once.Do(func() {
						entered <- struct{}{}
						switch mode {
						case "panic":
							panic("private panic")
						case "goexit":
							runtime.Goexit()
						default:
							<-release
						}
					})
				}}
				done := make(chan error, 1)
				go func() { done <- db.Transaction(t.Context(), func(*database.Tx) error { return failure }) }()
				if method == "Is" {
					// Scope classification only searches for framework database
					// errors; an application error's Is method is never invoked.
					select {
					case err := <-done:
						var detail *database.Error
						if errors.As(err, &detail) || state.committed.Load() != 0 || state.rolledBack.Load() != 1 || db.Stats().Owners != 0 {
							t.Fatal("application callback failure was relabeled or leaked ownership", err)
						}
					case <-time.After(time.Second):
						t.Fatal("transaction did not return")
					}
					return
				}
				select {
				case <-entered:
				case <-time.After(time.Second):
					t.Fatal("inspection did not start")
				}
				if mode == "blocked" {
					wait, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
					err := db.Close(wait)
					cancel()
					if !errors.Is(err, context.DeadlineExceeded) || db.Stats().Owners == 0 {
						t.Fatal("inspection abandoned pool owner")
					}
					release <- struct{}{}
				}
				select {
				case err := <-done:
					if state.committed.Load() != 0 || state.rolledBack.Load() != 1 {
						t.Fatal("inspection lost rollback", err)
					}
					var detail *database.Error
					if mode == "blocked" {
						// Completed inspection found no database failure: the
						// callback error keeps its identity after rollback.
						if errors.As(err, &detail) || err.Error() != failure.Error() {
							t.Fatal("application callback failure was relabeled", err)
						}
						break
					}
					if !errors.As(err, &detail) || detail.Outcome() != database.RolledBack || strings.Contains(err.Error(), "private") {
						t.Fatal("failed inspection lost conservative classification or redaction", err)
					}
				case <-time.After(time.Second):
					t.Fatal("inspection stranded transaction")
				}
			})
		}
	}
}
