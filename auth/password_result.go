package auth

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/database"
)

// PasswordResult retains the resolved model and verified proof. It is a trusted
// server result, not a response DTO. PendingMFA subjects still require their
// second factor; do not use Subject as an authorization shortcut. Ordinary
// guards reject that proof. A zero result has no valid proof. Keep this result
// within its server-side request; it is not a reusable reauthentication ticket.
type PasswordResult[M, K any] struct {
	subject M
	proof   Proof[M, K]
	recheck *passwordCheck[M]
}

// passwordCheck retains the original provider declaration and one shared model
// validator. Proof issuance and sensitive factor changes use this same check.
type passwordCheck[M any] struct {
	provider *declarationID
	verify   func(context.Context, *database.Tx) (M, error)
}

func (r PasswordResult[M, K]) Subject() M         { return r.subject }
func (r PasswordResult[M, K]) Proof() Proof[M, K] { return r.proof }
func (PasswordResult[M, K]) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("password authentication result"))
}
