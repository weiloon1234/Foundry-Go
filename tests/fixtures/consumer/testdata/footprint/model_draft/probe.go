package model_draft

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// Empty checks the replacement for a generated draft: an assignment list.
func Empty(assignments []orm.Assignment[models.LedgerEntry]) bool { return len(assignments) == 0 }
