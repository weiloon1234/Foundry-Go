package invalid

import (
	"foundry.test/goway/integrations"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func wrong(page integrations.CursorResult) {
	position, _ := page.Next().Get()
	var _ orm.Cursor[integrations.Response] = position
}
