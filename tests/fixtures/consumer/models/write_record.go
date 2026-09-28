package models

import (
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type RecordKey int64

//foundry:model table=write_records primary=Code
type WriteRecord struct {
	Code    RecordKey `foundry:"default=database"`
	Name    string
	Enabled bool `foundry:"default=database"`
	Note    value.Nullable[string]
	Amount  decimal.Decimal `foundry:"default=database"`
}
