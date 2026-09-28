package challenge_test

import (
	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	"github.com/weiloon1234/Foundry-Go/secret"
	"testing"
)

func TestRevisionPreventsRestoredValuesRevivingBindings(t *testing.T) {
	type member struct{}
	old, err := challenge.NewRevision[member]()
	if err != nil {
		t.Fatal(err)
	}
	next, err := challenge.NewRevision[member]()
	if err != nil {
		t.Fatal(err)
	}
	original, err := challenge.BindRevision(old, secret.New("a@example.test"))
	if err != nil {
		t.Fatal(err)
	}
	same, err := challenge.BindRevision(old, secret.New("a@example.test"))
	if err != nil || !same.Equal(original) {
		t.Fatal("stable state changed binding", err)
	}
	restored, err := challenge.BindRevision(next, secret.New("a@example.test"))
	if err != nil || restored.Equal(original) {
		t.Fatal("restored value revived old binding", err)
	}
	if _, err := challenge.BindRevision(challenge.Revision[member]{}, secret.New("a@example.test")); err == nil {
		t.Fatal("zero revision accepted")
	}
	if _, err := challenge.BindRevision(old); err == nil {
		t.Fatal("missing state accepted")
	}
	if _, err := challenge.BindRevision(old, make([]secret.String, 8)...); err == nil {
		t.Fatal("unbounded components accepted")
	}
}
