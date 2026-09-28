package invalid

import (
	"context"
	"foundry.test/consumer/httppagination"
	"foundry.test/consumer/models"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

var _ = httppagination.CursorList.Handle(func(context.Context, pagination.CursorRequest[foundryhttp.NoPath, httppagination.MemberFilters, models.User]) (httppagination.CursorListResult, error) {
	return httppagination.CursorListResult{}, nil
})
