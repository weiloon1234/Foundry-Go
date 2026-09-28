package lifecycle_test

import (
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestModelFieldComparisonPreservesModelAbsence(t *testing.T) {
	type account struct{ Name value.Nullable[string] }
	var before value.Optional[account]
	after := value.Set(account{})
	calls := 0
	change, err := lifecycle.CompareModelField(codec.Nullable(codec.String[string]()), before, after, false, func(v account) value.Nullable[string] {
		calls++
		return v.Name
	})
	if err != nil || calls != 1 || !change.Changed() || change.Before().IsSet() {
		t.Fatalf("model extraction lost absence: calls=%d err=%v", calls, err)
	}
	if name, present := change.After().Get(); !present || !name.IsNull() {
		t.Fatal("present model with NULL field became an absent model")
	}
	if _, err := lifecycle.CompareModelField(codec.String[string](), before, after, false, (func(account) string)(nil)); !errors.Is(err, fault.Invalid) {
		t.Fatalf("nil getter accepted: %v", err)
	}
}
