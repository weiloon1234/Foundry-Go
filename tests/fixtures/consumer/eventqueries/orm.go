package eventqueries

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/outbox"
)

// LinkTable retains the payload owner on persisted outbox IDs.
// Example: Links.MessageID.Eq(id).
type LinkTable struct {
	orm.Table[EventLink, int64]
	MessageID orm.Column[EventLink, outbox.ID[RecordCreated]]
}

// Links stores typed message references without granting outbox delivery authority.
// Example: Links.Insert(ctx,tx,Links.Key().Set(1),Links.MessageID.Set(id)).
var Links = orm.Define("event_links", func(b orm.Builder[EventLink], m *EventLink) LinkTable {
	return LinkTable{orm.TableOf(b, &m.ID), orm.IDOf(b, &m.MessageID)}
})
