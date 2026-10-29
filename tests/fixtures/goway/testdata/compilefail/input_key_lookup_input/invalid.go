package input_key_lookup_input

import orm "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	m "foundry.test/goway/models/mutators"
	"github.com/weiloon1234/Foundry-Go/database"
)

func invalid(db *database.DB) {
	_, _ = orm.Find(context.Background(), db, m.KeyRecords.Table, m.FreshID{})
}
