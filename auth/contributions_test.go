package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/plugin"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestPluginAuthorizationUsesExactTypedDeclarations(t *testing.T) {
	key := foundation.NewKey[*auth.Registry]("plugin.auth")
	guard := auth.DefineGuard("plugin.accounts", provider(func(_ context.Context, id accountKey) (value.Optional[account], error) {
		return value.Set(account{ID: id, Enabled: true}), nil
	}), strategy(t, "bearer", 7))
	policy := auth.DefinePolicy("plugin.read", func(_ context.Context, subject account, resource document) (bool, error) {
		return subject.ID == resource.Owner, nil
	})
	extension := plugin.Module{Declaration: plugin.Manifest{ID: "authorization", Version: "1.0.0", Framework: "*"}, OnRegister: func(r *plugin.Registrar) error {
		if err := auth.RegisterAuthorization(r, key, guard); err != nil {
			return err
		}
		return auth.RegisterAuthorization(r, key, policy)
	}}
	module := foundation.Module{Name: "auth", OnRegister: func(r *foundation.Registrar) error { return auth.RegisterRegistry(r, key, auth.DefaultConfig()) }}
	app, err := foundation.NewBuilder().Register(module).RegisterPlugin(extension).Build(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := foundation.Resolve(app.Services(), key)
	if err != nil {
		t.Fatal(err)
	}
	if err := policy.ValidateIn(registry); err != nil {
		t.Fatal(err)
	}
	s := scope(t, registry, auth.Credential{Name: "bearer", Secret: secret.New("valid")})
	if allowed, err := policy.Allows(s.Context(), guard, document{Owner: 7}); err != nil || !allowed {
		t.Fatal("contributed auth flow", allowed, err)
	}
	if allowed, err := policy.Allows(s.Context(), guard, document{Owner: 8}); err != nil || allowed {
		t.Fatal("contribution bypassed policy", allowed, err)
	}
	if _, err := foundation.NewBuilder().RegisterPlugin(extension).Build(t.Context()); !errors.Is(err, fault.Missing) {
		t.Fatal("orphan auth contribution accepted", err)
	}
}
