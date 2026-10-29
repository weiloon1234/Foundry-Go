package eventqueries

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/outbox"
)

// LinkTable retains the payload owner on persisted outbox IDs.
// Example: orm.ColumnEq(Links.MessageID, id).
type LinkTable struct {
	orm.Table[EventLink, int64]
	MessageID orm.Column[EventLink, outbox.ID[RecordCreated]]
}

// Links stores typed message references without granting outbox delivery authority.
// Example: orm.Insert(ctx, tx, Links.Table,orm.KeySet(Links.Key(), 1),orm.ColumnSet(Links.MessageID, id)).
var Links = orm.Define("event_links", func(b orm.Builder[EventLink], m *EventLink) LinkTable {
	return LinkTable{orm.TableOf(b, &m.ID), orm.IDOf(b, &m.MessageID)}
})
