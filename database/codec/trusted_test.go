package codec

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/internal/sqlcallback"
)

func codecIsolationDepth() int {
	var pcs [64]uintptr
	n := runtime.Callers(2, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])
	depth := 0
	for {
		frame, more := frames.Next()
		if strings.HasSuffix(frame.Function, "/internal/callback.Invoke") {
			depth++
		}
		if !more {
			return depth
		}
	}
}

func TestOnlyPrivateCodecCapabilitySkipsIsolation(t *testing.T) {
	for _, trusted := range []bool{false, true} {
		depth := -1
		encode := func(v int64) (driver.Value, error) { depth = codecIsolationDepth(); return v, nil }
		decode := func(v any) (int64, error) { depth = codecIsolationDepth(); return v.(int64), nil }
		c := New(encode, decode).WithParameterType(TypeInteger)
		if trusted {
			c = typed(TypeInteger, encode, decode)
		}
		for _, operation := range []string{"bind", "decode", "scan"} {
			var err error
			switch operation {
			case "bind":
				_, err = c.Bind(1)
			case "decode":
				_, err = c.Decode(int64(1))
			case "scan":
				var destination int64
				err = c.Scan(&destination).Scan(int64(1))
			}
			want := 1
			if trusted {
				want = 0
			}
			if err != nil || depth != want {
				t.Fatalf("%s trusted=%v: isolation depth %d, want %d", operation, trusted, depth, want)
			}
		}
	}
	// Public adapters cannot grant the capability to arbitrary application code.
	c := Signed[int64]()
	if !c.trusted || c.Validated(func(int64) error { return nil }).trusted || Adapt(c, func(v int64) int64 { return v }, func(v int64) int64 { return v }).trusted {
		t.Fatal("codec capability escaped its private constructor")
	}
	if allocations := testing.AllocsPerRun(100, func() {
		if v, err := c.Decode(int64(1)); err != nil || v != 1 {
			panic("decode failed")
		}
	}); allocations != 0 {
		t.Fatalf("built-in decode allocated: %v", allocations)
	}
}

func TestTrustedFailureKeepsIsolatedDiagnostic(t *testing.T) {
	// The same framework callback with/without the private capability must retain
	// exact error presentation, fault classification and cause identity.
	cause := safeerror.NewFault(fault.Invalid, "native codec rejected the value")
	encode := func(int64) (driver.Value, error) { return nil, cause }
	decode := func(any) (int64, error) { return 1, cause }
	trusted, isolated := typed(TypeInteger, encode, decode), New(encode, decode)
	_, bind1 := trusted.Bind(1)
	_, bind2 := isolated.Bind(1)
	_, decode1 := trusted.Decode(int64(1))
	_, decode2 := isolated.Decode(int64(1))
	var a, b int64
	scan1, scan2 := trusted.Scan(&a).Scan(int64(1)), isolated.Scan(&b).Scan(int64(1))
	native1 := sqlcallback.Destinations([]any{trusted.Scan(&a)})[0].(sql.Scanner).Scan(int64(1))
	native2 := sqlcallback.Destinations([]any{isolated.Scan(&b)})[0].(sql.Scanner).Scan(int64(1))
	for _, pair := range [][2]error{{bind1, bind2}, {decode1, decode2}, {scan1, scan2}, {safeerror.Reveal("scan", native1), safeerror.Reveal("scan", native2)}} {
		if pair[0].Error() != pair[1].Error() || !errors.Is(pair[0], cause) || !errors.Is(pair[0], fault.Invalid) {
			t.Fatal("trusted callback diagnostic or identity changed")
		}
	}
}
