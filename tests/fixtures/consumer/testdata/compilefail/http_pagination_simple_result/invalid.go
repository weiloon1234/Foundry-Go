package invalid

import (
	"context"
	"foundry.test/consumer/httppagination"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

var _ = httppagination.List.Handle(func(context.Context, httppagination.ListRequest) (query.SimplePage[httppagination.MemberResponse], error) {
	return query.SimplePage[httppagination.MemberResponse]{}, nil
})
