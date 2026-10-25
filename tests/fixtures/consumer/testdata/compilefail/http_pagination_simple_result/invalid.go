package invalid

import (
	"context"
	"foundry.test/consumer/httppagination"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

var _ = httppagination.List.Handle(func(context.Context, httppagination.ListRequest) (orm.SimplePage[httppagination.MemberResponse], error) {
	return orm.SimplePage[httppagination.MemberResponse]{}, nil
})
