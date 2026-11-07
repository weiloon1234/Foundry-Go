package background

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/application"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/webhook/outbound"
)

// doc-example:begin prune-owned-service
var WebhooksKey = foundation.NewKey[*outbound.Service]("background.webhooks")

func WebhookPruning(application.Services) (application.FeatureDeclarations, error) {
	return application.FeatureDeclarations{Pruning: []application.Pruning{
		application.PruneService("webhooks.deliveries", outbound.MaxPruneBatch, WebhooksKey,
			func(ctx context.Context, service *outbound.Service, limit int) (int64, error) {
				removed, err := service.PruneDeliveries(ctx, 30*24*time.Hour, limit)
				return int64(removed), err
			}),
	}}, nil
}

func ConfigureWebhookPruning(builder *application.Builder, requires []foundation.ProviderID, construct func(foundation.Resolver) (*outbound.Service, error)) *application.Builder {
	module := outbound.Module("background.webhooks", WebhooksKey, requires, construct)
	return builder.Register(module).Features(WebhookPruning)
}

// doc-example:end prune-owned-service

// doc-example:begin prune-retained-service
func RetainedWebhookPruning(service *outbound.Service) application.Pruning {
	return application.PruneWith("webhooks.deliveries", outbound.MaxPruneBatch, func(ctx context.Context, limit int) (int64, error) {
		removed, err := service.PruneDeliveries(ctx, 30*24*time.Hour, limit)
		return int64(removed), err
	})
}

// doc-example:end prune-retained-service
