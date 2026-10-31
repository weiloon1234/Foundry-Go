package invalid

import (
	"context"
	"foundry.test/goway/integrations"
	"foundry.test/goway/models"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

var _ = pagination.Handle(integrations.Cursor, func(context.Context, pagination.CursorRequest[foundryhttp.NoPath, integrations.Filters, models.Order]) (integrations.CursorResult, error) {
	return integrations.CursorResult{}, nil
})
