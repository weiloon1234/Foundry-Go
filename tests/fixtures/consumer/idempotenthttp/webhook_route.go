package idempotenthttp

import (
	"context"
	"fmt"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/database"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/idempotency"
	"github.com/weiloon1234/Foundry-Go/webhook"
)

// doc-example:begin verified-webhook-route
type ProviderAccount int64

func WebhookRouter(verifier *webhook.Verifier[ProviderAccount], store *idempotency.Store, record func(context.Context, *database.Tx, Submission) error) (*foundryhttp.Router, error) {
	route := foundryhttp.DefineRoute(foundryhttp.RouteSpec{ID: "provider.webhook", Method: foundryhttp.POST, Access: foundryhttp.Public}, foundryhttp.StaticPath("/webhook"))
	endpoint := foundryhttp.DefineEndpoint(route, foundryhttp.EmptyQuery(), SubmissionBody(), foundryhttp.EmptyResponse(204))
	endpoint = foundryhttp.ConfigureEndpoint(endpoint, foundryhttp.Use(foundryhttp.VerifyWebhook(verifier)))
	idempotent := foundryhttp.Idempotent(endpoint, store, idempotency.Definition{ID: "provider.delivery", Version: 1})

	type input = foundryhttp.Input[foundryhttp.NoPath, foundryhttp.NoQuery, Submission]
	registration := idempotent.Handle(func(ctx context.Context, _ input) (idempotency.Scope, error) {
		delivery, ok := verifier.FromContext(ctx)
		if !ok {
			return idempotency.Scope{}, fmt.Errorf("verified delivery missing")
		}
		return idempotency.NewScope(string(delivery.Provider()), strconv.FormatInt(int64(delivery.Account()), 10))
	}, func(ctx context.Context, tx *database.Tx, in input) (foundryhttp.NoContent, error) {
		return foundryhttp.NoContent{}, record(ctx, tx, in.Body)
	})
	return foundryhttp.NewRouter(registration)
}

// doc-example:end verified-webhook-route
