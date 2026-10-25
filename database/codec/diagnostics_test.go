package codec_test

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type privateCodecDiagnostic struct {
	exit  bool
	calls atomic.Int32
}

func (e *privateCodecDiagnostic) Error() string {
	e.calls.Add(1)
	if e.exit {
		runtime.Goexit()
	}
	panic("private codec diagnostic")
}
func (e *privateCodecDiagnostic) Format(fmt.State, rune) { _ = e.Error() }

func TestCodecPipelineDiagnostics(t *testing.T) {
	for _, op := range []struct {
		name   string
		run    func(codec.Codec[string]) error
		prefix string
	}{
		{"bind", func(c codec.Codec[string]) error { _, err := c.Bind("\x00"); return err }, "database value binding"},
		{"decode", func(c codec.Codec[string]) error { _, err := c.Decode([]byte{0}); return err }, "database value decoding"},
		{"scan", func(c codec.Codec[string]) error { var v string; return c.Scan(&v).Scan([]byte{0}) }, "database value scanning"},
	} {
		for _, owned := range []bool{false, true} {
			for _, exit := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/owned=%t/goexit=%t", op.name, owned, exit), func(t *testing.T) {
					cause := &privateCodecDiagnostic{exit: exit}
					native := codec.String[string]()
					var original error
					c := codec.New(func(v string) (driver.Value, error) {
						if owned {
							_, original = native.Bind(v)
						} else {
							original = fault.Wrap(fault.Invalid, "private codec", cause)
						}
						return nil, original
					}, func(v any) (string, error) {
						if owned {
							_, original = native.Decode(v)
						} else {
							original = fault.Wrap(fault.Invalid, "private codec", cause)
						}
						return "", original
					})
					err := op.run(c)
					want := op.prefix + " failed"
					if owned {
						want = op.prefix + ": " + original.Error()
					}
					var retained *fault.Error
					if err == nil || err.Error() != want || !errors.Is(err, original) || !errors.Is(err, fault.Invalid) || !errors.As(err, &retained) {
						t.Fatal("codec lost diagnostic or identity", err)
					}
					if !owned {
						var private *privateCodecDiagnostic
						if !errors.Is(err, cause) || !errors.As(err, &private) || private != cause {
							t.Fatal("codec lost application cause")
						}
					}
					_ = fmt.Sprintf("%v %+v %#v", err, err, err)
					if cause.calls.Load() != 0 {
						t.Fatal("codec formatted application error")
					}
				})
			}
		}
	}
}
