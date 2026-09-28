package errorgraph_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/weiloon1234/Foundry-Go/internal/errorgraph"
)

type cycle struct{}

func (*cycle) Error() string   { return "cycle" }
func (c *cycle) Unwrap() error { return c }

type nonComparable []string

func (nonComparable) Error() string { return "slice" }

func TestWalkRetainsOrderAndBoundsMalformedGraphs(t *testing.T) {
	first, second := errors.New("first"), errors.New("second")
	joined := errors.Join(first, second)
	var visited []error
	if !errorgraph.Walk(joined, func(err error) bool { visited = append(visited, err); return true }) || !slices.Equal(visited, []error{joined, first, second}) {
		t.Fatal("error order changed")
	}
	count := 0
	if errorgraph.Walk(&cycle{}, func(error) bool { count++; return true }) || count > 256 {
		t.Fatal("cycle exceeded traversal budget")
	}
	count = 0
	if !errorgraph.Walk(&cycle{}, func(error) bool { count++; return false }) || count != 1 {
		t.Fatal("early stop unwrapped a completed match")
	}
	if !errorgraph.Walk(nil, func(error) bool { t.Fatal("visited nil"); return true }) {
		t.Fatal("nil error exceeded budget")
	}
	if errorgraph.Matches(nonComparable{"x"}, nonComparable{"x"}) {
		t.Fatal("non-comparable errors compared equal")
	}
	if !errorgraph.Matches(first, first) || errorgraph.Matches(first, second) {
		t.Fatal("error equality changed")
	}
}

type customMatch struct{ target error }

func (customMatch) Error() string          { return "custom" }
func (e customMatch) Is(target error) bool { return target == e.target }

func TestIsMatchesWithinBoundsWithoutUnwrappingAnEstablishedMatch(t *testing.T) {
	target := errors.New("target")
	cyclic := &cycle{}
	for _, test := range []struct {
		err, target error
		want        bool
	}{
		{nil, nil, true}, {target, nil, false}, {nil, target, false},
		{target, target, true}, {cyclic, target, false},
		{errors.Join(errors.New("other"), customMatch{target}), target, true},
		{errors.Join(target, cyclic), target, true},
		{nonComparable{"x"}, nonComparable{"x"}, false},
	} {
		if got := errorgraph.Is(test.err, test.target); got != test.want {
			t.Fatal("bounded match changed", got, test.want)
		}
	}
}

type typedMatch struct{ id int }

func (*typedMatch) Error() string { return "typed" }

type customAs struct{}

func (customAs) Error() string { return "custom As" }
func (customAs) As(target any) bool {
	if selected, ok := target.(**typedMatch); ok {
		*selected = &typedMatch{}
		return true
	}
	return false
}

func TestHasRetainsAssignableAndCustomMatchesWithBounds(t *testing.T) {
	for _, test := range []struct {
		err  error
		want bool
	}{
		{nil, false}, {&cycle{}, false}, {&typedMatch{}, true},
		{errors.Join(errors.New("other"), customAs{}), true},
		{errors.Join(&typedMatch{}, &cycle{}), true},
	} {
		if got := errorgraph.Has[*typedMatch](test.err); got != test.want {
			t.Fatal("bounded typed match changed", got, test.want)
		}
	}
}

func TestAsReturnsTheFirstValueAndReportsExhaustion(t *testing.T) {
	first, second := &typedMatch{id: 1}, &typedMatch{id: 2}
	for _, test := range []struct {
		err             error
		want            *typedMatch
		found, complete bool
	}{
		{nil, nil, false, true},
		{&cycle{}, nil, false, false},
		{errors.Join(first, second, &cycle{}), first, true, true},
		{errors.Join(&cycle{}, first), nil, false, false},
		{(*typedMatch)(nil), nil, true, true},
	} {
		value, found, complete := errorgraph.As[*typedMatch](test.err)
		if value != test.want || found != test.found || complete != test.complete {
			t.Fatal("bounded typed lookup changed value, presence or completion")
		}
	}
	if value, found, complete := errorgraph.As[*typedMatch](customAs{}); value == nil || !found || !complete {
		t.Fatal("custom As value was lost")
	}
}
