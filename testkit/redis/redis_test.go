package redis

import (
	"testing"

	frameredis "github.com/weiloon1234/Foundry-Go/redis"
)

func TestConfigCredentials(t *testing.T) {
	for _, tc := range []struct {
		name, user, password string
	}{
		{"neither", "", ""},
		{"both", "test-user", "test-password"},
		{"password only", "", "test-password"},
		{"user only", "test-user", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(AddressVariable, "127.0.0.1:6379")
			t.Setenv(UserVariable, tc.user)
			t.Setenv(PasswordVariable, tc.password)
			config := Config(t)
			if config.User != tc.user {
				t.Fatalf("user = %q, want %q", config.User, tc.user)
			}
			if config.Password.Reveal() != tc.password {
				t.Fatal("password was not read into the secret-bearing field")
			}
			if config.Password.IsZero() != (tc.password == "") {
				t.Fatal("an empty password must stay unset")
			}
			if limits := frameredis.DefaultConfig(); config.MaxConnections != limits.MaxConnections {
				t.Fatal("credentials must not change the default client limits")
			}
		})
	}
}
