package http_test

import (
	"errors"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"testing"
)

func TestInvalidAdapterRegistrationPreservesCause(t *testing.T) {
	cause := errors.New("invalid adapter declaration")
	if _, err := foundryhttp.NewRouter(foundryhttp.InvalidRouteRegistration(cause)); !errors.Is(err, cause) {
		t.Fatalf("declaration cause lost: %v", err)
	}
	if _, err := foundryhttp.NewRouter(foundryhttp.InvalidRouteRegistration(nil)); err == nil {
		t.Fatal("nil declaration accepted")
	}
}
