// Package query builds typed PostgreSQL queries from model and projection
// declarations. Generated model facades retain concrete model, key, field,
// draft and result types. Complete reads use declared codecs and retrieval
// callbacks; writes preserve their declared lifecycle and transaction ownership.
//
// Stored-field computations use [Value], [Param], [CountField],
// [CountDistinctField], [Min], [Max], [Sum] and [Avg]. Computed expressions and
// aggregates retain their own Value and Param methods. Row and selected
// expressions preserve separate owner, value and nullability contracts.
//
// Generated declarations and explicit integrations supply model metadata and
// complete decoders through [Define] and [ForModel]. [For] can describe a table
// without providing an executable model decoder. Model-independent SQL planning
// is private implementation; consumers use this package's typed declarations.
package query
