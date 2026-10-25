package seed

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
)

type outcomeInspectionError struct {
	method, mode     string
	calls, formatted int
}

func (e *outcomeInspectionError) Error() string          { e.formatted++; panic("private seed error") }
func (e *outcomeInspectionError) Format(fmt.State, rune) { e.formatted++; panic("private seed format") }
func (e *outcomeInspectionError) As(target any) bool {
	if e.method == "nil" {
		if result, ok := target.(**database.Error); ok {
			*result = nil
			return true
		}
	}
	if e.method == "As" {
		e.inspect()
	}
	return false
}
func (e *outcomeInspectionError) Unwrap() error {
	if e.method != "Unwrap" {
		return nil
	}
	e.inspect()
	if e.mode == "cycle" && e.calls < 512 {
		return e
	}
	return nil
}
func (e *outcomeInspectionError) inspect() {
	e.calls++
	switch e.mode {
	case "panic":
		panic("private seed inspection")
	case "goexit":
		runtime.Goexit()
	}
}
func TestSeederOutcomeInspectionFailsClosed(t *testing.T) {
	for _, method := range []string{"As", "Unwrap"} {
		for _, mode := range []string{"panic", "goexit", "cycle"} {
			t.Run(method+"/"+mode, func(t *testing.T) {
				cause := &outcomeInspectionError{method: method, mode: mode}
				if committed(cause) {
					t.Fatal("unconfirmed seeder reported committed")
				}
				if cause.calls == 0 || cause.calls > 65 || cause.formatted != 0 {
					t.Fatal("inspection was unbounded or formatted application error")
				}
			})
		}
	}
	if committed(&outcomeInspectionError{method: "nil"}) {
		t.Fatal("nil custom As result proved commit")
	}
	if committed(database.NewError("seed", database.QueryFailed)) {
		t.Fatal("unconfirmed database error proved commit")
	}
}
