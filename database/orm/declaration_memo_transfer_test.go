package orm

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"sync"
	"sync/atomic"
	"testing"
)

func TestDeclarationValidationRunsOnceAndRetainsFailure(t *testing.T) {
	for _, panics := range []bool{false, true} {
		d := scopeTransferFields()
		var calls atomic.Int32
		sentinel := errors.New("declaration failure")
		d.core.columns[0].check = func() error {
			calls.Add(1)
			if panics {
				panic("private declaration panic")
			}
			return sentinel
		}
		var wg sync.WaitGroup
		for range 8 {
			wg.Go(func() {
				e := Check(d)
				if panics {
					if !errors.Is(e, fault.Panicked) {
						t.Error(e)
					}
				} else if !errors.Is(e, sentinel) {
					t.Error(e)
				}
			})
		}
		wg.Wait()
		if calls.Load() != 1 {
			t.Fatal("declaration checked repeatedly", calls.Load())
		}
	}
}
