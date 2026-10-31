package invalid

import (
	"context"
	"foundry.test/goway/integrations"
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

var _ = pagination.Handle(integrations.Cursor, func(context.Context, integrations.CursorRequest) (pagination.CursorResult[models.Order, integrations.Response], error) {
	return pagination.CursorResult[models.Order, integrations.Response]{}, nil
})
