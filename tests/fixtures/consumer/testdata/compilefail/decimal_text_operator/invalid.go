package invalid

import "foundry.test/consumer/models"

var invalid = models.LedgerEntries.Amount.Contains("1")
