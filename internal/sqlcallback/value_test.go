package sqlcallback

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"runtime"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

type pointerScanner struct{ value any }

func (s *pointerScanner) Scan(value any) error { s.value = value; return nil }
func TestIndirectScannerPreservesAllocationAndNull(t *testing.T) {
	var destination *pointerScanner
	scan := Destinations([]any{&destination})[0].(sql.Scanner)
	if err := scan.Scan(int64(12)); err != nil || destination == nil || destination.value != int64(12) {
		t.Fatal("indirect scanner was not allocated")
	}
	if err := scan.Scan(nil); err != nil || destination != nil {
		t.Fatal("SQL NULL did not reset indirect scanner")
	}
	var direct pointerScanner
	if err := Destinations([]any{&direct})[0].(sql.Scanner).Scan(nil); err != nil || direct.value != nil {
		t.Fatal("direct scanner NULL behavior changed")
	}
}

type nilValuer struct{}

func (nilValuer) Value() (driver.Value, error) { panic("nil value receiver should not be called") }
func TestIndirectNilValuerPreservesNull(t *testing.T) {
	var inner *nilValuer
	outer := &inner
	adapted := Argument(outer)
	value, err := driver.DefaultParameterConverter.ConvertValue(adapted)
	if value != nil || err != nil {
		t.Fatal("nil promoted valuer changed")
	}
}

type exitScanner struct{}

func (exitScanner) Scan(any) error { runtime.Goexit(); return nil }
func TestScannerGoexitContained(t *testing.T) {
	err := Destinations([]any{exitScanner{}})[0].(sql.Scanner).Scan("value")
	if err == nil {
		t.Fatal("Goexit became success")
	}
}
func TestDriverControlErrorPassesThrough(t *testing.T) {
	for _, control := range []error{driver.ErrBadConn, driver.ErrSkip, driver.ErrRemoveArgument} {
		if got := call("driver control", func() error { return control }); got != control {
			t.Fatal("driver control changed")
		}
	}
	cause := errors.New("private")
	returned := call("driver error", func() error { return cause })
	if errors.Is(returned, cause) {
		t.Fatal("driver cause exposed inside SQL")
	}
	if !errors.Is(safeerror.Reveal("driver failed", returned), cause) {
		t.Fatal("driver cause lost after SQL")
	}
}

type cyclicArgument *cyclicArgument

func TestSQLPointerCyclesFailClosed(t *testing.T) {
	var cycle cyclicArgument
	cycle = cyclicArgument(&cycle)
	adapted := Argument(cycle)
	if _, err := driver.DefaultParameterConverter.ConvertValue(adapted); err == nil {
		t.Fatal("cyclic argument accepted")
	}
	destination := Destinations([]any{&cycle})[0].(sql.Scanner)
	if err := destination.Scan(int64(1)); err == nil {
		t.Fatal("cyclic destination accepted")
	}
}
