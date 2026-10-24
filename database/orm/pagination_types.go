package orm

import "github.com/weiloon1234/Foundry-Go/database/query"

// MaxPageSize is the shared pagination bound. Example: size <= orm.MaxPageSize.
const MaxPageSize = query.MaxPageSize

// Cursor is the existing owner-typed position token, preserving its wire identity.
// Example: var next orm.Cursor[User].
type Cursor[M any] = query.Cursor[M]

// CursorRequest preserves the existing bounded keyset request. Example: orm.CursorRequest[User]{Size:20}.
type CursorRequest[M any] = query.CursorRequest[M]

// CursorPage preserves the existing keyset response. Example: page.Next.Get().
type CursorPage[M any] = query.CursorPage[M]

// CursorInputError preserves typed cursor error classification. Example: var invalid *orm.CursorInputError.
type CursorInputError = query.CursorInputError

// ParseCursor parses a position without granting access to its source. Example: orm.ParseCursor[User](token).
func ParseCursor[M any](token string) (Cursor[M], error) { return query.ParseCursor[M](token) }
