package validationrules_test

import (
	"context"
	"errors"
	"foundry.test/consumer/validationrules"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
	"sync/atomic"
	"testing"
)

type addresses struct{ calls atomic.Int32 }

func (*addresses) Validate() error { return nil }
func (a *addresses) Exists(_ context.Context, v string) (bool, error) {
	a.calls.Add(1)
	return v == "taken@example.test", nil
}
func TestExpandedRegistrationChecks(t *testing.T) {
	lookup := &addresses{}
	rule := validationrules.RegistrationChecks(lookup)
	input := validationrules.Registration{Kind: validationrules.Business, Email: "free@example.test", Company: value.Set("Team"), Tags: []string{"go-team"}}
	if err := rule.Check(t.Context(), input, validation.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	input.Email = "invalid"
	input.Tags = []string{"bad tag"}
	input.Company = value.Optional[string]{}
	var rejected *validation.Errors
	if !errors.As(rule.Check(t.Context(), input, validation.DefaultLimits()), &rejected) || len(rejected.Issues()) != 3 {
		t.Fatal("missing field rejections")
	}
	if lookup.calls.Load() != 1 {
		t.Fatal("syntax failure reached remote lookup")
	}
	info, err := rule.Description()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = info.Normalize(); err != nil {
		t.Fatal(err)
	}
}
