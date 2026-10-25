package invalid

import (
	"context"
	"foundry.test/consumer/httppagination"
	"foundry.test/consumer/models"
	"foundry.test/consumer/mutatorqueries"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/http/pagination"
)

func wrong() {
	_, _ = pagination.MapPage[mutatorqueries.Member, httppagination.MemberResponse](context.Background(), orm.PageResult[models.User]{}, httppagination.PresentMember)
}
