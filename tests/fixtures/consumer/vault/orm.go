package vault

// EntryTable exposes plaintext setters and preserves row-bound encryption.
// Example: orm.Set(Entries.Token, encrypted.NewText("secret")).

// Entries owns encrypted hydration and persistence. Example: orm.RequireKey(ctx, db, Entries.Table,id).
