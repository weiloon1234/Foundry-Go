package passwords_test

import (
	"foundry.test/consumer/passwords"
	"github.com/weiloon1234/Foundry-Go/auth/password"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"strings"
	"testing"
)

func TestPasswordDTOAndHashConsumer(t *testing.T) {
	limits := foundryhttp.DefaultEndpointLimits().Response
	input, err := passwords.LoginRequestJSON().Decode(t.Context(), []byte(`{"email":"user@example.test","password":"exact password"}`), limits)
	if err != nil {
		t.Fatal(err)
	}
	hasher, err := password.New(password.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	stored, err := passwords.HashForRegistration(t.Context(), hasher, input)
	if err != nil {
		t.Fatal(err)
	}
	if match, err := passwords.CheckPassword(t.Context(), hasher, input, stored); err != nil || !match {
		t.Fatal("typed password check", err)
	}
	if needs, err := passwords.ChangedHashPolicy(hasher, stored); err != nil || needs {
		t.Fatal("current policy", err)
	}
	encoded, err := passwords.LoginRequestJSON().Encode(t.Context(), input, limits)
	if err != nil || strings.Contains(string(encoded), "exact password") {
		t.Fatal("login DTO disclosed password", err)
	}
}
