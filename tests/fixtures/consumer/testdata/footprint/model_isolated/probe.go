package model_isolated

import "foundry.test/consumer/models"

func Has(e models.LedgerEntry) bool { return e.ID != models.LedgerEntry{}.ID }
