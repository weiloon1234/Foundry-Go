package modelvalidation

import (
	"foundry.test/consumer/models"
	"foundry.test/consumer/softqueries"
)

// Validation uses the model owner’s columns and stored key types.
var (
	Users         = models.Users
	Countries     = models.Countries
	LedgerEntries = models.LedgerEntries
	Groups        = softqueries.Groups
)
