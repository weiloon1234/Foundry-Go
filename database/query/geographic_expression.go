package query

import (
	"math"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// GeographicPoint is a validated latitude/longitude pair in degrees. Its zero
// value is the valid origin (0, 0); construction rejects non-finite/out-of-range input.
type GeographicPoint struct{ latitude, longitude float64 }

func NewGeographicPoint(latitude, longitude float64) (GeographicPoint, error) {
	if math.IsNaN(latitude) || math.IsInf(latitude, 0) || latitude < -90 || latitude > 90 || math.IsNaN(longitude) || math.IsInf(longitude, 0) || longitude < -180 || longitude > 180 {
		return GeographicPoint{}, fault.New(fault.Invalid, "invalid geographic coordinates")
	}
	return GeographicPoint{latitude, longitude}, nil
}

// SphericalDistance returns great-circle metres on the IUGG mean-radius sphere
// (6371008.8 m). Stored coordinates must be valid degrees. This is straight-line
// surface distance, not a route estimate. The expression retains its row owner.
func SphericalDistance[S any, V numericValue](latitude, longitude RowValue[S, V], origin GeographicPoint) OrderedRowExpression[S, float64] {
	return orderedRow(SphericalDistanceValue(FloatValue(rowInput(latitude).Value()), FloatValue(rowInput(longitude).Value()), origin))
}

// SphericalDistanceValue composes selected floating coordinates.
func SphericalDistanceValue[S any](latitude, longitude Expression[S, float64], origin GeographicPoint) Expression[S, float64] {
	return operationValue[S](sphericalDistanceOperation, codec.Float[float64](), operationArg(latitude), operationArg(longitude), operationArg(latitude.Param(origin.latitude).Value()), operationArg(longitude.Param(origin.longitude).Value()))
}

// SphericalDistanceNullable propagates NULL when either stored coordinate is absent.
func SphericalDistanceNullable[S any, V numericValue](latitude, longitude RowValue[S, value.Nullable[V]], origin GeographicPoint) NullableOrderedRowExpression[S, float64] {
	return nullableOrderedRow(SphericalDistanceNullableValue(FloatNullableValue(rowInput(latitude).Value()), FloatNullableValue(rowInput(longitude).Value()), origin))
}

// SphericalDistanceNullableValue is the selected nullable counterpart.
func SphericalDistanceNullableValue[S any](latitude, longitude Expression[S, value.Nullable[float64]], origin GeographicPoint) Expression[S, value.Nullable[float64]] {
	return operationValue[S](sphericalDistanceOperation, codec.Nullable(codec.Float[float64]()), operationArg(latitude), operationArg(longitude), operationArg(latitude.Param(value.Of(origin.latitude)).Value()), operationArg(longitude.Param(value.Of(origin.longitude)).Value()))
}

