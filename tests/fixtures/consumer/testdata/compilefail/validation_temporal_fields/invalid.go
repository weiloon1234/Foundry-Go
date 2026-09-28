package invalid

import (
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/validation"
)

type Input struct {
	Date    temporal.Date
	Instant temporal.DateTime
}

var date = validation.DefineField("date", func(v Input) temporal.Date { return v.Date })
var instant = validation.DefineField("instant", func(v Input) temporal.DateTime { return v.Instant })
var _ = validation.BeforeField(date, instant)
