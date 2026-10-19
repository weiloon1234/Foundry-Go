// Package querycore owns Foundry's non-generic SQL AST, validation and PostgreSQL
// planning/compiler implementation. The public database/query package checks
// concrete model, key, value, result and callback types before lowering here.
//
// Exported engine data is internal to the framework. Typed public facades hold it
// in private named fields; it must never become a public raw-query escape hatch.
// Keep this package independent of database/query and application assembly so
// implementation-only edits can reuse consumer compilation and cache artifacts.
package querycore
