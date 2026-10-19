# Geographic distance

`query.NewGeographicPoint(latitude, longitude)` validates finite coordinates in
degrees (latitude −90…90, longitude −180…180). Zero is a valid coordinate.
`query.SphericalDistance(fields.Latitude, fields.Longitude, origin)` retains the
row owner and accepts typed numeric fields, including exact decimal coordinates.
Use `.Asc()` with the model ID as a stable tie-breaker before numbered pagination;
use `.Lte(metres)` for a radius filter. Origin coordinates are bound parameters.

`SphericalDistanceNullable` returns NULL when either field is NULL. Nullable
ordering should explicitly place NULL last. Selected-value counterparts preserve
aggregate/window composition. Stored coordinates require database range checks.

The result is approximate surface distance in metres on the IUGG mean-radius
sphere (6371008.8 metres), not driving distance. Haversine clamps roundoff and
handles the antimeridian and antipodes. The compiler uses ordinary PostgreSQL
math functions; PostGIS and application SQL fragments are unnecessary. Existing
expression ownership, depth, parameter and SQL-expansion bounds still apply.
