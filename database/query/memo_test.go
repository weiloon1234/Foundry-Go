package query

import (
	"sync"
	"testing"
)

func TestMemoBuildsOnceAndRepeatsPanics(t *testing.T) {
	var memo Memo[int]
	calls := 0
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if memo.Get(func() int { calls++; return 42 }) != 42 {
				t.Error("memoized value changed")
			}
		})
	}
	wg.Wait()
	if calls != 1 {
		t.Fatal("declaration built more than once", calls)
	}
	var failing Memo[int]
	for range 2 {
		func() {
			defer func() {
				if recover() != "declaration bug" {
					t.Error("build panic was not repeated")
				}
			}()
			failing.Get(func() int { panic("declaration bug") })
		}()
	}
}
