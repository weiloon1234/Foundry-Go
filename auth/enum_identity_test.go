package auth

import (
	"errors"
	"testing"
)

func TestBareDecisionRetainsEnumIdentity(t *testing.T) {
	var code Code
	if !errors.As(Unauthenticated.WithCause(nil), &code) || code != Unauthenticated {
		t.Fatal("auth enum identity lost")
	}
}
