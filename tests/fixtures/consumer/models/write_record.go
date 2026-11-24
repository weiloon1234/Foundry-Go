package models

import (
	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/value"
)

type RecordKey int64

//foundry:model table=write_records var=WriteRecords key=Code identity=false
type WriteRecord struct {
	Code    RecordKey `foundry:"default=database,member=Code,key-role=primary"`
	Name    string
	Enabled bool `foundry:"default=database,role=column"`
	Note    value.Nullable[string]
	Amount  decimal.Decimal `foundry:"default=database,role=number"`
}
