package invalid

import "foundry.test/consumer/models"

var invalid = models.LedgerEntryDraft{}.SetAmount(1.25)
