package invalid

import (
	"context"
	"foundry.test/goway/integrations"
	"foundry.test/goway/models"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

var _ = integrations.Cursor.Handle(func(context.Context, pagination.CursorRequest[foundryhttp.NoPath, integrations.Filters, models.Order]) (integrations.CursorResult, error) {
	return integrations.CursorResult{}, nil
})
