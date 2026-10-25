package idempotency

import (
	"errors"
	"testing"
)

func TestInvalidKeyRetainsEnumIdentity(t *testing.T) {
	_, err := ParseKey("")
	var code Code
	if !errors.As(err, &code) || code != BadKey {
		t.Fatal("idempotency enum identity lost")
	}
}
