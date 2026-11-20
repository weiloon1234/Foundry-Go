package invalid

import (
	"foundry.test/goway/models/scalars"
	"github.com/weiloon1234/Foundry-Go/datatable"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/validation"
	"github.com/weiloon1234/Foundry-Go/value"
)

type row struct {
	T value.Nullable[temporal.DateTime]
}

var _ = datatable.NullableTime(validation.DefineField("t", func(r row) value.Nullable[temporal.DateTime] { return r.T }), "f.t", scalars.Records.Instant, foundryhttp.TextQuery[temporal.DateTime, *temporal.DateTime]())
