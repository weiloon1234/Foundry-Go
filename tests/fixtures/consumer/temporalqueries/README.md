# Temporal query acceptance

`Samples` declares the existing temporal fields, including the numeric primary
role and nullable temporal codecs. `CalendarSummaries` declares the unchanged
report result. Tests use explicit `expr.Value`/`expr.At` bindings for calendar
operations, CTEs, correlations and windows; PostgreSQL assertions preserve time
zones, DST, precision, bounds, nullability and transaction-time semantics.

Generated model/projection APIs remain only for the publication barrier and the
explicit SQL parity oracle.
