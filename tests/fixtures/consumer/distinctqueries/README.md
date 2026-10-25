# DISTINCT consumer acceptance

ORM scalar GROUP BY/HAVING/DISTINCT/DISTINCT ON and complete record/set DISTINCT
retain typed values, nullable results, enum codecs and CTE membership. The runtime
tests cover count/first/exists bounds, unselected DISTINCT ON keys, ordering
precedence, cancellation and rejection before I/O. Old/new SQL parity includes
parameter binding and nested SELECT boundaries.

Only `TestPostgresDistinctRecordStreaming` remains on the legacy API, with a TODO
for lane A's complete-record stream terminal. Its original streamed-row assertion
remains active. The lead runs PostgreSQL; local tests check SQL and invalid inputs.
