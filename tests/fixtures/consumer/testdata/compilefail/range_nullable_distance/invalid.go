package invalid

import (
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/value"
)

var _ = query.NullableNumericRange(query.WindowFor(models.QueryMeasurements()), models.MeasurementFields().Score.Value()).Preceding(value.Of(float64(1)))
