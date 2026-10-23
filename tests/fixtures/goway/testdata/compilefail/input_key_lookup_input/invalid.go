package input_key_lookup_input

import (
	"context"
	m "foundry.test/goway/models/mutators"
	"github.com/weiloon1234/Foundry-Go/database"
)

func invalid(db *database.DB) { _, _ = m.KeyRecords.Find(context.Background(), db, m.FreshID{}) }
