package validation_test

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/validation"
)

type textLookup struct {
	validate func() error
	find     func(context.Context, string) (bool, error)
}

func (l *textLookup) Validate() error {
	if l.validate != nil {
		return l.validate()
	}
	return nil
}
func (l *textLookup) Exists(ctx context.Context, input string) (bool, error) {
	return l.find(ctx, input)
}

func TestTypedLookupsShareSafeAdvisoryRuleBehavior(t *testing.T) {
	t.Parallel()
	lookup := &textLookup{find: func(_ context.Context, input string) (bool, error) { return input == "taken", nil }}
	exists, unique := validation.Exists[string](lookup), validation.Unique[string](lookup)
	for _, tc := range []struct {
		name     string
		rule     validation.Rule[string]
		input    string
		accepted bool
	}{
		{"exists", exists, "taken", true}, {"missing", exists, "free", false},
		{"available", unique, "free", true}, {"taken", unique, "taken", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.rule.Check(t.Context(), tc.input, validation.DefaultLimits())
			if tc.accepted {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			issues := rejection(t, err).Issues()
			if len(issues) != 1 || issues[0].Message == "" || strings.Contains(issues[0].Message, tc.input) {
				t.Fatalf("unsafe rejection: %+v", issues)
			}
		})
	}
	info, err := unique.Description()
	if err != nil || !info.ServerOnly || info.Spec.ID != "foundry.unique" || len(info.Spec.Parameters) != 0 {
		t.Fatalf("lookup metadata: %+v, %v", info, err)
	}
	// Separate lookup instances are the same built-in rule, not conflicting
	// custom definitions. Both may be used in one request.
	if err := validation.All(unique, validation.Unique[string](&textLookup{find: lookup.find})).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLookupFailuresNeverMeanAvailable(t *testing.T) {
	t.Parallel()
	private := errors.New("private database failure")
	for _, check := range []func(context.Context, string) (bool, error){
		func(context.Context, string) (bool, error) { return false, private },
		func(context.Context, string) (bool, error) { panic("private panic") },
		func(context.Context, string) (bool, error) { runtime.Goexit(); return false, nil },
	} {
		rule := validation.Unique[string](&textLookup{find: check})
		err := rule.Check(t.Context(), "secret-input", validation.DefaultLimits())
		var rejected *validation.Errors
		if !errors.Is(err, fault.Internal) || errors.As(err, &rejected) || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "secret-input") {
			t.Fatalf("lookup failure was not safe: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	rule := validation.Exists[string](&textLookup{find: func(received context.Context, _ string) (bool, error) { cancel(); return false, received.Err() }})
	if err := rule.Check(ctx, "value", validation.DefaultLimits()); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestLookupDeclarationsFailBeforeChecks(t *testing.T) {
	t.Parallel()
	var absent *textLookup
	for _, lookup := range []validation.Lookup[string]{
		nil, absent,
		&textLookup{validate: func() error { return errors.New("invalid") }},
		&textLookup{validate: func() error { panic("invalid") }},
		&textLookup{validate: func() error { runtime.Goexit(); return nil }},
	} {
		if validation.Exists(lookup).Validate() == nil {
			t.Fatal("invalid lookup declaration accepted")
		}
	}
}
