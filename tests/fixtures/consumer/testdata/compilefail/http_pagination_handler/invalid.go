package invalid

import (
	"context"
	"foundry.test/consumer/httpdto"
	"foundry.test/consumer/httppagination"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

var _ = pagination.Handle(httppagination.List, func(context.Context, httppagination.ListRequest) (orm.PageResult[httpdto.UserResponse], error) {
	return orm.PageResult[httpdto.UserResponse]{}, nil
})
