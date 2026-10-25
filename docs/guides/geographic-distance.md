# Geographic distance

<!-- compiled-examples -->

`expr.NewGeographicPoint(latitude, longitude)` validates finite degree coordinates
(latitude −90…90, longitude −180…180). Zero is valid. `expr.SphericalDistance`
accepts typed numeric row operands, such as `expr.Value(column)`, and a validated
origin. Exact decimal operands are supported. Origin coordinates are parameters.

Use the result's `.Lte(metres)` with `expr.Where` for a radius filter and `.Asc()`
with `expr.OrderBy` for distance order. Add a unique final key before pagination.
The [compiled consumer](../../tests/fixtures/consumer/scalarqueries/geographic_test.go)
checks construction and compilation; applications should declare separate
latitude/longitude fields and enforce stored-coordinate range constraints.

`SphericalDistanceNullable` returns NULL when either operand is NULL. Selected
forms preserve aggregate/window phase. Result ordering can explicitly place NULL
last. No implicit zero coordinate substitutes for absence.

The result is approximate surface distance in metres using a 6,371,008.8-metre
sphere, not driving distance. Haversine clamps roundoff and handles antimeridian
and antipodal points. Compilation uses ordinary PostgreSQL math functions, without
PostGIS or application SQL fragments. Expression ownership and compiler budgets
still apply.

For declaration and API migration, see the [ORM migration guide](orm-migration.md).
