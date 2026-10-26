package querycore

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

type privateCursorFailure struct{ formatted int }

func (e *privateCursorFailure) Error() string { e.formatted++; return "private cursor failure" }
func (e *privateCursorFailure) Format(s fmt.State, _ rune) {
	e.formatted++
	fmt.Fprint(s, "private cursor failure")
}

func TestCursorCodecRedactionPreservesClassification(t *testing.T) {
	scope := strings.Repeat("a", 64)
	token, err := MakeCursor(scope, []driver.Value{int64(7)})
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"encode", "decode", "validate"} {
		for _, invalid := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/invalid=%v", phase, invalid), func(t *testing.T) {
				private := &privateCursorFailure{}
				var cause error = private
				if invalid {
					cause = fault.Wrap(fault.Invalid, "invalid key", private)
				}
				c := codec.New(func(v int64) (driver.Value, error) {
					if phase == "encode" {
						return nil, cause
					}
					return v, nil
				}, func(any) (int64, error) {
					if phase == "decode" {
						return 0, cause
					}
					return 7, nil
				})
				if phase == "validate" {
					c = c.Validated(func(int64) error { return cause })
				}
				_, err := ReadCursorBoundary(token, true, false, scope, []CursorField{{Decode: func(raw any) (driver.Value, error) {
					v, err := c.Decode(raw)
					if err != nil {
						return nil, err
					}
					return c.Bind(v)
				}}}, []bool{true})
				var input *CursorInputError
				var retained *privateCursorFailure
				if err == nil || errors.As(err, &input) != invalid || errors.Is(err, fault.Invalid) != invalid || !errors.Is(err, private) || !errors.As(err, &retained) || retained != private {
					t.Fatal("cursor codec classification or cause changed")
				}
				_ = fmt.Sprintf("%v %+v %#v", err, err, err)
				if private.formatted != 0 {
					t.Fatal("cursor cause formatted")
				}
			})
		}
	}
}
func TestCursorBoundaryDistinguishesInputFromMetadataFailures(t *testing.T) {
	scope := strings.Repeat("a", 64)
	token, err := MakeCursor(scope, []driver.Value{int64(7)})
	if err != nil {
		t.Fatal(err)
	}
	field := CursorField{Decode: func(raw any) (driver.Value, error) { return codec.Signed[int64]().Decode(raw) }}
	for _, required := range [][]bool{nil, {true, false}} {
		_, err := ReadCursorBoundary(token, true, false, scope, []CursorField{field}, required)
		var input *CursorInputError
		if err == nil || errors.As(err, &input) {
			t.Fatal("metadata failure classified as input")
		}
	}
	_, err = ReadCursorBoundary(token, true, false, scope, []CursorField{{}}, []bool{true})
	var input *CursorInputError
	if err == nil || errors.As(err, &input) {
		t.Fatal("missing decoder classified as input")
	}
	for _, bad := range []struct {
		scope string
		key   driver.Value
	}{{"different", int64(7)}, {scope, "private incompatible key"}, {scope, nil}} {
		token, err := MakeCursor(bad.scope, []driver.Value{bad.key})
		if bad.scope == "different" {
			token, err = MakeCursor(strings.Repeat("b", 64), []driver.Value{bad.key})
		}
		if err != nil {
			t.Fatal(err)
		}
		_, err = ReadCursorBoundary(token, true, false, scope, []CursorField{field}, []bool{true})
		if !errors.As(err, &input) || !errors.Is(err, fault.Invalid) || input.Error() != "invalid cursor input or query scope" {
			t.Fatal("bad boundary classification")
		}
	}
}
func TestCursorCodecFailuresRemainServerErrors(t *testing.T) {
	scope := strings.Repeat("a", 64)
	token, err := MakeCursor(scope, []driver.Value{int64(7)})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"error", "panic", "goexit", "invalid", "zero"} {
		t.Run(mode, func(t *testing.T) {
			cause := errors.New("private codec failure")
			c := codec.New[int64](func(v int64) (driver.Value, error) { return v, nil }, func(any) (int64, error) {
				switch mode {
				case "panic":
					panic("private")
				case "goexit":
					runtime.Goexit()
				case "invalid":
					return 0, fault.New(fault.Invalid, "invalid key")
				}
				return 0, cause
			})
			field := CursorField{Decode: func(raw any) (driver.Value, error) {
				v, err := c.Decode(raw)
				if err != nil {
					return nil, err
				}
				return c.Bind(v)
			}}
			// Invalid declaration metadata has no decoder; native table checks
			// reject the zero codec before building this runtime adapter.
			if mode == "zero" {
				field = CursorField{}
			}
			_, err := ReadCursorBoundary(token, true, false, scope, []CursorField{field}, []bool{true})
			var input *CursorInputError
			if err == nil || errors.As(err, &input) != (mode == "invalid") {
				t.Fatal("codec classification changed")
			}
			if mode == "error" && !errors.Is(err, cause) {
				t.Fatal("codec cause lost")
			}
		})
	}
}
func TestCursorResourceBounds(t *testing.T) {
	if _, err := MakeCursor(strings.Repeat("a", 64), []driver.Value{strings.Repeat("x", MaxCursorBytes+1)}); !errors.Is(err, fault.Invalid) {
		t.Fatal("oversized output cursor accepted", err)
	}
}

type cyclicCursorFailure struct{ visits atomic.Int32 }

func (*cyclicCursorFailure) Error() string { panic("private cursor failure must not be formatted") }
func (e *cyclicCursorFailure) Unwrap() error {
	if e.visits.Add(1) > 4096 {
		return nil
	}
	return e
}
func TestCyclicCursorCodecErrorRemainsServerFailure(t *testing.T) {
	scope := strings.Repeat("a", 64)
	token, err := MakeCursor(scope, []driver.Value{int64(7)})
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []bool{false, true} {
		cycle := new(cyclicCursorFailure)
		var failure error = cycle
		if invalid {
			failure = errors.Join(fault.Invalid, cycle)
		}
		fields := []CursorField{{Decode: func(raw any) (driver.Value, error) {
			if failure != nil {
				return nil, failure
			}
			return codec.Signed[int64]().Decode(raw)
		}}}
		boundary, err := ReadCursorBoundary(token, true, false, scope, fields, []bool{true})
		if err == nil || len(boundary.Keys) != 0 {
			t.Fatal("failed codec published boundary")
		}
		visits := cycle.visits.Load()
		_, input, _ := errorgraph.As[*CursorInputError](err)
		if input != invalid || visits > 256 || !invalid && visits == 0 {
			t.Fatal("cursor classification lost bounds")
		}
		failure = nil
		boundary, err = ReadCursorBoundary(token, true, false, scope, fields, []bool{true})
		if err != nil || len(boundary.Keys) != 1 || boundary.Keys[0] != int64(7) {
			t.Fatal("healthy codec did not recover")
		}
	}
}
