package orm

import "github.com/weiloon1234/Foundry-Go/internal/querycore"

// MaxInsertRows bounds rows in one atomic insert or upsert statement. The
// statement-wide MaxParameters limit can require smaller batches.
const MaxInsertRows = querycore.MaxInsertRows

// MaxIdentityBatch bounds keys in one identity observation or extension scope.
const MaxIdentityBatch = querycore.MaxIdentityBatch

// DefaultChunkSize is the closed-batch size used by model iteration with eager
// relations or retrieval callbacks.
const DefaultChunkSize = querycore.DefaultChunkSize

// MaxCursorBytes bounds the encoded cursor token accepted by ParseCursor.
const MaxCursorBytes = querycore.MaxCursorBytes

// MaxCursorFields bounds the ordered fields in one cursor key.
const MaxCursorFields = querycore.MaxCursorFields

// MaxExpressionDepth bounds nested expression and SELECT compilation.
const MaxExpressionDepth = querycore.MaxExpressionDepth

// MaxExpressionNodes bounds expression and SELECT compilation work.
const MaxExpressionNodes = querycore.MaxExpressionNodes

// MaxParameters bounds bound parameters across the whole SQL statement.
const MaxParameters = querycore.MaxParameters

// MaxMembershipValues bounds the values in one membership predicate.
const MaxMembershipValues = querycore.MaxMembershipValues

// MaxScalarSQLBytes bounds SQL emitted by computed scalar operations.
const MaxScalarSQLBytes = querycore.MaxScalarSQLBytes
