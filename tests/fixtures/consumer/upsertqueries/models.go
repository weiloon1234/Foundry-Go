package upsertqueries

type ExcludedKey int64

type ExcludedRecord struct {
	ID   ExcludedKey `foundry:"default=database"`
	Name string      `foundry:"default=database"`
}
