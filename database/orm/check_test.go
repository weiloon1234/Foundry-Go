package orm

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestCheckCollectsErrorsAroundPromotedNil(t *testing.T) {
	type row struct {
		ID   int64
		Name string
	}
	invalid := func(name string) Table[row, int64] {
		return Define(name, func(b Builder[row], m *row) Table[row, int64] {
			return TableOf(b, &m.ID)
		})
	}
	type promoted struct{ *Table[row, int64] }
	type embedded struct{ CheckedTable }
	for _, broken := range []CheckedTable{promoted{}, embedded{}} {
		err := Check(invalid("before_failure"), broken, invalid("after_failure"))
		if !errors.Is(err, fault.Panicked) {
			t.Fatal("missing contained declaration failure", err)
		}
		for _, want := range []string{"before_failure.Name", "after_failure.Name"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("missing declaration error %q: %v", want, err)
			}
		}
	}
}

type checkCallback func() error

func (f checkCallback) ormError() error { return f() }

func TestCheckContinuesAfterCallbackExit(t *testing.T) {
	first, last := errors.New("first declaration"), errors.New("last declaration")
	for _, mode := range []string{"panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			err := Check(
				checkCallback(func() error { return first }),
				checkCallback(func() error {
					if mode == "goexit" {
						runtime.Goexit()
					}
					panic("private declaration value")
				}),
				checkCallback(func() error { return last }),
			)
			if !errors.Is(err, first) || !errors.Is(err, last) || !errors.Is(err, fault.Panicked) {
				t.Fatal("declaration failures were not aggregated", err)
			}
			if strings.Contains(err.Error(), "private declaration value") {
				t.Fatal("panic payload disclosed")
			}
		})
	}
}
