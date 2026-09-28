package validation_test

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/validation"
	"testing"
)

type batchSource struct {
	calls int
	fail  error
}

func (*batchSource) Validate() error { return nil }
func (s *batchSource) AllExist(_ context.Context, values []string) (bool, error) {
	s.calls++
	values[0] = "changed"
	return false, s.fail
}
func TestBatchLookupOwnsInputAndFailsClosed(t *testing.T) {
	source := &batchSource{}
	rule := validation.ExistsAll[[]string](source)
	input := []string{"original"}
	rejection(t, rule.Check(t.Context(), input, validation.DefaultLimits()))
	if input[0] != "original" {
		t.Fatal("lookup mutated caller slice")
	}
	source.fail = errors.New("private")
	if err := rule.Check(t.Context(), input, validation.DefaultLimits()); !errors.Is(err, fault.Internal) {
		t.Fatal(err)
	}
	if err := rule.Check(t.Context(), nil, validation.DefaultLimits()); err != nil || source.calls != 2 {
		t.Fatal("empty input performed IO", err)
	}
	if validation.ExistsAll[[]string](nil).Validate() == nil {
		t.Fatal("nil batch lookup accepted")
	}
}
