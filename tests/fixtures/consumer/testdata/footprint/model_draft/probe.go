package model_draft

import "foundry.test/consumer/models"

func Empty(d models.LedgerEntryDraft) bool { return d.IsEmpty() }
