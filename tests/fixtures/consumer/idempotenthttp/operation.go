package idempotenthttp

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/idempotency"
)

// Runner exposes the same typed core for commands and verified provider adapters.
func Runner(store *idempotency.Store) (idempotency.Operation[Submission, Receipt], error) {
	return idempotency.Define(store, idempotency.Definition{ID: "orders.command", Version: 1}, idempotency.JSONInput(SubmissionJSON()), idempotency.JSONEncoding(ReceiptJSON()))
}
func Execute(ctx context.Context, store *idempotency.Store, scope idempotency.Scope, key idempotency.Key, in Submission, handler idempotency.Handler[Submission, Receipt]) (idempotency.Result[Receipt], error) {
	op, err := Runner(store)
	if err != nil {
		return idempotency.Result[Receipt]{}, err
	}
	return op.Run(ctx, scope, key, in, handler)
}
