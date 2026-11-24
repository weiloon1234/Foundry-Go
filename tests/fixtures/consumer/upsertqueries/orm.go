package upsertqueries

// ExcludedRecordTable keeps database defaults and the distinct stored primary key.
// Example: orm.Set(ExcludedRecords.Name, "before").

// ExcludedRecords exercises quoting of the physical table named excluded.
// Example: orm.Upsert(ctx,tx,ExcludedRecords.Query(),policy,assignments...).
