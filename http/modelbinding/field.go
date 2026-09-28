package modelbinding

import (
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
)

// ByField resolves a stored alternate key through a model-owned generated field.
// Filters, eager loads, hooks and soft-delete scope remain owned by the query.
// A duplicate key is an internal error, never an arbitrary first row. The supplied
// executor may be a transaction; this adapter starts no transaction or row lock.
func ByField[P, M any, K comparable](executor database.Executor, source query.ModelQuerySource[M], field query.KeyField[M, K], key func(P) K) Resolver[P, M] {
	return ByKey(executor, query.Unique(source, field), key)
}
