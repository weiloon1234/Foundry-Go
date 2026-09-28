package session

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestCheckedProofAndJoinedRevocationRequireBackendCapabilities(t *testing.T) {
	backend := &fakeBackend{create: func(context.Context, Address, Creation) (Record, error) {
		t.Error("unsupported backend received unchecked creation")
		return Record{}, nil
	}}
	bound := binding(t, backend, settings())
	verified := proof(t)
	checked, err := verified.WithIssuanceCheck(func(context.Context, *database.Tx) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	issued, err := bound.Issue(t.Context(), checked, IssueOptions{})
	if !errors.Is(err, fault.Invalid) || !issued.Secret().IsZero() {
		t.Fatal("unsupported checked proof accepted", err)
	}
	if _, err := bound.Revocation(); !errors.Is(err, fault.Invalid) {
		t.Fatal("unsupported contribution accepted", err)
	}
	if n, err := bound.RevokeAllIn(t.Context(), &database.Tx{}, member{7}.reference()); !errors.Is(err, fault.Invalid) || n != 0 {
		t.Fatal("unsupported joined revocation accepted", err)
	}
}
