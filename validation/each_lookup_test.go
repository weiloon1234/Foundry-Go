package validation_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/validation"
)

// catalogLookup records every batch it observes and mutates its argument to
// prove rules pass owned copies.
type catalogLookup struct {
	stored  map[string]bool
	batches [][]string
	fail    error
}

func (*catalogLookup) Validate() error { return nil }
func (l *catalogLookup) AllExist(_ context.Context, values []string) (bool, error) {
	l.batches = append(l.batches, slices.Clone(values))
	all := true
	for i, value := range values {
		all = all && l.stored[value]
		values[i] = "changed"
	}
	return all, l.fail
}
func (l *catalogLookup) AnyExist(_ context.Context, values []string) (bool, error) {
	l.batches = append(l.batches, slices.Clone(values))
	for _, value := range values {
		if l.stored[value] {
			return true, l.fail
		}
	}
	return false, l.fail
}

type orderLine struct{ Product string }
type order struct{ Lines []orderLine }

func productCodes(n int, missing ...int) []string {
	values := make([]string, n)
	for i := range values {
		values[i] = "stored"
		if slices.Contains(missing, i) {
			values[i] = "missing"
		}
	}
	return values
}

func TestExistsAllReportsElementPathsWithOneObservationWhenValid(t *testing.T) {
	t.Parallel()
	lookup := &catalogLookup{stored: map[string]bool{"stored": true}}
	rule := validation.ExistsAll[[]string](lookup)
	input := productCodes(100)
	if err := rule.Check(t.Context(), input, validation.DefaultLimits()); err != nil || len(lookup.batches) != 1 {
		t.Fatalf("valid input used %d observations: %v", len(lookup.batches), err)
	}
	if input[0] != "stored" {
		t.Fatal("lookup mutated caller input")
	}
	lookup.batches = nil
	issues := rejection(t, rule.Check(t.Context(), productCodes(100, 3, 97), validation.DefaultLimits())).Issues()
	if len(issues) != 2 || issues[0].Path != "/3" || issues[1].Path != "/97" || issues[0].Code != "foundry.exists_all" {
		t.Fatalf("element paths: %+v", issues)
	}
	// Two misses among 100 are located by bisection, not 100 observations.
	if len(lookup.batches) > 30 {
		t.Fatalf("rejection used %d observations", len(lookup.batches))
	}
	limits := validation.DefaultLimits()
	limits.Issues = 2
	capped := rejection(t, rule.Check(t.Context(), productCodes(10, 0, 1, 2, 3), limits))
	if len(capped.Issues()) != 2 || !capped.Truncated() {
		t.Fatalf("issue cap: %+v", capped.Issues())
	}
	lookup.fail = errors.New("private")
	if err := rule.Check(t.Context(), productCodes(4, 2), validation.DefaultLimits()); !errors.Is(err, fault.Internal) {
		t.Fatal("lookup failure was not an execution failure", err)
	}
}

func TestEachLookupsSelectFieldsAndReportNestedPaths(t *testing.T) {
	t.Parallel()
	lookup := &catalogLookup{stored: map[string]bool{"stored": true, "taken": true}}
	product := validation.DefineField("product_id", func(line orderLine) string { return line.Product }).WithLabel("Product")
	lines := validation.DefineField("lines", func(input order) []orderLine { return input.Lines })
	rules := lines.Rules(validation.ExistsEach[[]orderLine](product, lookup))
	input := order{Lines: []orderLine{{"stored"}, {"stored"}, {"missing"}, {"stored"}}}
	issues := rejection(t, rules.Check(t.Context(), input, validation.DefaultLimits())).Issues()
	if len(issues) != 1 || issues[0].Path != "/lines/2/product_id" || issues[0].Code != "foundry.exists" || issues[0].Label != "Product" {
		t.Fatalf("each path/label: %+v", issues)
	}
	if issues[0].Message != "Product has an invalid selection." {
		t.Fatalf("each message: %q", issues[0].Message)
	}
	unique := lines.Rules(validation.UniqueEach[[]orderLine](product, lookup))
	lookup.batches = nil
	if err := unique.Check(t.Context(), order{Lines: []orderLine{{"new"}, {"other"}}}, validation.DefaultLimits()); err != nil || len(lookup.batches) != 1 {
		t.Fatal("unique each used per-element I/O", err)
	}
	issues = rejection(t, unique.Check(t.Context(), order{Lines: []orderLine{{"new"}, {"taken"}, {"other"}}}, validation.DefaultLimits())).Issues()
	if len(issues) != 1 || issues[0].Path != "/lines/1/product_id" || issues[0].Code != "foundry.unique" {
		t.Fatalf("unique each: %+v", issues)
	}
	all := validation.UniqueAll[[]string](lookup)
	issues = rejection(t, all.Check(t.Context(), []string{"new", "taken"}, validation.DefaultLimits())).Issues()
	if len(issues) != 1 || issues[0].Path != "/1" || issues[0].Code != "foundry.unique_all" {
		t.Fatalf("unique all: %+v", issues)
	}
	info, err := rules.Description()
	if err != nil || !info.ServerOnly || info.Children[0].Children[0].Spec.Parameters[0].Name != "field" {
		t.Fatalf("each metadata: %+v %v", info, err)
	}
	if _, err := info.Normalize(); err != nil {
		t.Fatal(err)
	}
	var zero validation.Field[orderLine, string]
	for _, bad := range []validation.Rule[[]orderLine]{
		validation.ExistsEach[[]orderLine](zero, lookup),
		validation.ExistsEach[[]orderLine, orderLine, string](product, nil),
		validation.UniqueEach[[]orderLine, orderLine, string](product, nil),
	} {
		if bad.Validate() == nil {
			t.Fatal("invalid each declaration accepted")
		}
	}
	limits := validation.DefaultLimits()
	limits.Checks = 3
	var bounded *validation.LimitError
	lookup.batches = nil
	if err := rules.Check(t.Context(), order{Lines: make([]orderLine, 10)}, limits); !errors.As(err, &bounded) || len(lookup.batches) != 0 {
		t.Fatal("over-budget collection performed I/O", err)
	}
}
