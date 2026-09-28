package invalid

import (
	"foundry.test/consumer/httppagination"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

func wrong(page httppagination.CursorListResult) {
	position, _ := page.Next().Get()
	var _ query.Cursor[httppagination.MemberResponse] = position
}
