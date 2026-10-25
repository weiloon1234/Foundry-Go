# DISTINCT consumer acceptance

ORM scalar GROUP BY/HAVING/DISTINCT/DISTINCT ON and complete record/set DISTINCT
retain typed values, nullable results, enum codecs and CTE membership. The runtime
tests cover count/first/exists bounds, unselected DISTINCT ON keys, ordering
precedence, cancellation and rejection before I/O. Old/new SQL parity includes
parameter binding and nested SELECT boundaries.

Complete-record DISTINCT streaming uses `rows.EachRead`, retaining the original
streamed-row assertion. The lead runs PostgreSQL; local tests check SQL and invalid inputs.
