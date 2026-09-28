package upsertqueries

type ExcludedKey int64

//foundry:model table=excluded primary=ID
type ExcludedRecord struct {
	ID   ExcludedKey `foundry:"default=database"`
	Name string      `foundry:"default=database"`
}
