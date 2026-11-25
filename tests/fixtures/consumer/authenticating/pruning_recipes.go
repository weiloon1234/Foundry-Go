package authenticating

import (
	"context"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/schedule"
)

// doc-example:begin prune-sessions-hourly
func pruneSessionsHourly(calendar schedule.Calendar, web *session.Sessions[models.User, model.ID[models.User]]) (schedule.Declaration, error) {
	return calendar.Hourly("auth.sessions.prune", func(ctx context.Context, _ schedule.Invocation) error {
		_, err := web.PruneExpired(ctx, 1024, 16)
		return err
	})
}

// doc-example:end prune-sessions-hourly

// doc-example:begin prune-tokens-hourly
func pruneTokensHourly(calendar schedule.Calendar, api *token.Tokens[models.User, model.ID[models.User]]) (schedule.Declaration, error) {
	return calendar.Hourly("auth.tokens.prune", func(ctx context.Context, _ schedule.Invocation) error {
		_, err := api.PruneExpired(ctx, token.MaxPruneFamilies, 16)
		return err
	})
}

// doc-example:end prune-tokens-hourly
