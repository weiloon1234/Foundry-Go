// Package cache binds semantic cache declarations to concrete Go key and value
// types. Applications use Cache methods; serialized bytes remain in Backend
// adapters. A missing entry is distinct from a stored zero value and an error.
package cache
