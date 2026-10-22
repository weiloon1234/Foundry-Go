package int_text_key

import (
	"foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

func F() { orm.TextKey(models.WriteRecords.Key()) }
