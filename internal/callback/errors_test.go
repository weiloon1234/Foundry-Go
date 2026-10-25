package callback_test

import (
	"errors"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/errorgraphtest"
	"testing"
)

func TestMatchingContainsHostileMethods(t *testing.T) {
	for _, mode := range []string{"cycle", "panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			cause := &errorgraphtest.Error{Mode: mode}
			sentinel := errors.New("target")
			var target interface {
				error
				Missing()
			}
			if callback.ErrorIs(cause, sentinel) || callback.ErrorAs(cause, &target) || callback.ErrorWalk(cause, func(error) bool { return true }) {
				t.Fatal("incomplete inspection established a match")
			}
			if target != nil || cause.Formatted.Load() != 0 || cause.Visits.Load() > 256 {
				t.Fatal("inspection escaped bounds or formatted")
			}
		})
	}
}

type nilDetail struct{}

func (*nilDetail) Error() string { return "nil detail" }
func TestNilAsDetailDoesNotEstablishClassification(t *testing.T) {
	var original *nilDetail
	var target *nilDetail
	if callback.ErrorAs(original, &target) {
		t.Fatal("typed nil established a usable detail")
	}
	if matched, failure := callback.ErrorAsResult(original, &target); matched || failure == nil {
		t.Fatal("nil detail was treated as a completed nonmatch")
	}
}
