package invalid

import (
	"context"
	"foundry.test/consumer/httppagination"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

var _ = httppagination.CursorList.Handle(func(context.Context, httppagination.CursorListRequest) (pagination.CursorResult[models.User, httppagination.MemberResponse], error) {
	return pagination.CursorResult[models.User, httppagination.MemberResponse]{}, nil
})
