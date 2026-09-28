package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
)

func TestRevocationDeclarationsRejectDuplicateAndDifferentProviders(t *testing.T) {
	provider := passwordProvider(func(context.Context, passwordSubject) (bool, error) { return true, nil })
	action := func(context.Context, *database.Tx, model.Reference[passwordSubject, int64]) (uint64, error) {
		return 0, nil
	}
	first := auth.DefineRevocation("sessions.web", provider, action)
	if _, err := auth.NewRevocations(provider, first, first); !errors.Is(err, fault.Duplicate) {
		t.Fatal("duplicate stores accepted", err)
	}
	other := passwordProvider(func(context.Context, passwordSubject) (bool, error) { return true, nil })
	if _, err := auth.NewRevocations(provider, auth.DefineRevocation("tokens.api", other, action)); err == nil {
		t.Fatal("different provider accepted")
	}
	if _, err := auth.NewRevocations(provider); err == nil {
		t.Fatal("empty revocation set accepted")
	}
	group, err := auth.NewRevocations(provider, first, auth.DefineRevocation("tokens.api", provider, action))
	if err != nil {
		t.Fatal(err)
	}
	if err := group.Invalidate(t.Context(), nil, passwordSubject{ID: 1}); err == nil {
		t.Fatal("missing transaction accepted")
	}
}
