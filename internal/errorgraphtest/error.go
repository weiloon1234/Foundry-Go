// Package errorgraphtest supplies adversarial errors for framework regressions.
package errorgraphtest

import (
	"fmt"
	"runtime"
	"sync/atomic"
)

type Error struct {
	Mode              string
	Visits, Formatted atomic.Int32
}

func (e *Error) Error() string          { e.Formatted.Add(1); panic("private error formatted") }
func (e *Error) Format(fmt.State, rune) { _ = e.Error() }
func (e *Error) inspect() {
	switch e.Mode {
	case "panic":
		panic("private inspection")
	case "goexit":
		runtime.Goexit()
	}
}
func (e *Error) Is(error) bool { e.inspect(); return false }
func (e *Error) As(any) bool   { e.inspect(); return false }
func (e *Error) Unwrap() error {
	e.inspect()
	// Escape only to keep a broken unbounded implementation from stranding CI.
	if e.Visits.Add(1) > 4096 {
		return nil
	}
	return e
}
func (e *Error) Read([]byte) (int, error)          { return 0, e }
func (e *Error) ReadAt([]byte, int64) (int, error) { return 0, e }
