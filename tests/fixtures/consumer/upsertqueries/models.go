package upsertqueries

type ExcludedKey int64

//foundry:model table=excluded var=ExcludedRecords key=ID identity=false
type ExcludedRecord struct {
	ID   ExcludedKey `foundry:"default=database"`
	Name string      `foundry:"default=database"`
}
