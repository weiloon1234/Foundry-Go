package eventqueries

// EventLinkTable retains the payload owner on persisted outbox IDs.
// Example: orm.Eq(Links.MessageID, id).

// Links stores typed message references without granting outbox delivery authority.
// Example: orm.Insert(ctx, tx, Links.Table,orm.Set(Links.Key(), 1),orm.Set(Links.MessageID, id)).
