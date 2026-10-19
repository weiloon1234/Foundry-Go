package query

import (
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
	"math"
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

func sphericalDistanceSQL(a []string) string {
	// Explicit NULL guard is necessary: PostgreSQL LEAST/GREATEST ignore NULL.
	// Clamp haversine to [0, 1] to tolerate roundoff at coincident/antipodal points.
	h := "(POWER(SIN(RADIANS(" + a[0] + " - " + a[2] + ") / 2.0), 2) + COS(RADIANS(" + a[0] + ")) * COS(RADIANS(" + a[2] + ")) * POWER(SIN(RADIANS(" + a[1] + " - " + a[3] + ") / 2.0), 2))"
	return "(CASE WHEN " + a[0] + " IS NULL OR " + a[1] + " IS NULL THEN NULL ELSE 12742017.6 * ASIN(SQRT(LEAST(1.0, GREATEST(0.0, " + h + ")))) END)"
}
