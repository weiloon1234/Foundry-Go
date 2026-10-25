package invalid

import (
	"foundry.test/consumer/httppagination"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func wrong(page httppagination.CursorListResult) {
	position, _ := page.Next().Get()
	var _ orm.Cursor[httppagination.MemberResponse] = position
}
