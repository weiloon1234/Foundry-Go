package invalid

import "foundry.test/consumer/models"

var invalid = models.LedgerEntryFields().Amount.Contains("1")
