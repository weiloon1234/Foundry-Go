// Package framequeries exercises typed numeric and calendar window distances.
package framequeries

import (
	"time"

	"github.com/weiloon1234/Foundry-Go/decimal"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=frame_samples var=Samples key=ID identity=false
type Sample struct {
	ID     int `foundry:"member=ID,key-role=primary-number"`
	Band   int16
	Amount decimal.Decimal `foundry:"role=number"`
	Score  value.Nullable[float32]
	At     time.Time
	Date   temporal.Date
	Local  temporal.LocalDateTime
	Clock  temporal.Time
}
