package auth_test

import (
	"context"
	"errors"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/fault"
	"testing"
)

func TestPermissionLabelsPreserveExactAuthorityIdentity(t *testing.T) {
	check := func(context.Context, account) (bool, error) { return true, nil }
	permission := auth.DefinePermission("accounts.manage", check)
	labeled := permission.WithLabelKey("permissions.accounts.manage")
	r := registry(t, permission.Registration())
	if err := labeled.ValidateIn(r); err != nil {
		t.Fatal("label replaced registered policy", err)
	}
	description, err := labeled.Description()
	if err != nil || description.Name != permission.Name() || description.LabelKey != "permissions.accounts.manage" || permission.LabelKey() != "" {
		t.Fatal(description, err)
	}
	other := auth.DefinePermission("accounts.manage", check).WithLabelKey(labeled.LabelKey())
	if err := other.ValidateIn(r); !errors.Is(err, fault.Missing) {
		t.Fatal("same label granted authority", err)
	}
	if _, err := auth.NewRegistry(auth.DefaultConfig(), permission.WithLabelKey("bad key").Registration()); err == nil {
		t.Fatal("registration skipped label validation")
	}
	if (auth.Permission[account]{}).WithLabelKey("permissions.x").Validate() == nil {
		t.Fatal("label made zero declaration valid")
	}
}
