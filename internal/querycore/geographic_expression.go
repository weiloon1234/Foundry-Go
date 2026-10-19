package querycore

func sphericalDistanceSQL(a []string) string {
	// Explicit NULL guard is necessary: PostgreSQL LEAST/GREATEST ignore NULL.
	// Clamp haversine to [0, 1] to tolerate roundoff at coincident/antipodal points.
	h := "(POWER(SIN(RADIANS(" + a[0] + " - " + a[2] + ") / 2.0), 2) + COS(RADIANS(" + a[0] + ")) * COS(RADIANS(" + a[2] + ")) * POWER(SIN(RADIANS(" + a[1] + " - " + a[3] + ") / 2.0), 2))"
	return "(CASE WHEN " + a[0] + " IS NULL OR " + a[1] + " IS NULL THEN NULL ELSE 12742017.6 * ASIN(SQRT(LEAST(1.0, GREATEST(0.0, " + h + ")))) END)"
}
