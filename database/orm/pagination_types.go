package orm

import "github.com/weiloon1234/Foundry-Go/internal/querycore"

// MaxPageSize is the shared pagination bound. Example: size <= orm.MaxPageSize.
const MaxPageSize = querycore.MaxPageSize

// Cursor is the existing owner-typed position token, preserving its wire identity.
// Example: var next orm.Cursor[User].
type Cursor[M any] = querycore.Cursor[M]

// CursorRequest preserves the existing bounded keyset request. Example: orm.CursorRequest[User]{Size:20}.
type CursorRequest[M any] = querycore.CursorRequest[M]

// CursorPage preserves the existing keyset response. Example: page.Next.Get().
type CursorPage[M any] = querycore.CursorPage[M]

// CursorInputError preserves typed cursor error classification. Example: var invalid *orm.CursorInputError.
type CursorInputError = querycore.CursorInputError

// ParseCursor parses a position without granting access to its source. Example: orm.ParseCursor[User](token).
func ParseCursor[M any](token string) (Cursor[M], error) { return querycore.ParseCursor[M](token) }
