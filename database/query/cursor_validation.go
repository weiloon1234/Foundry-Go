package query

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// CursorInputError identifies a malformed request boundary or a token that does
// not belong to the current query. Adapters can classify this input failure
// separately from invalid query declarations or failed output token creation.
// It never formats a supplied token, decoded sort key or database value.
type CursorInputError struct{ cause error }

func (*CursorInputError) Error() string        { return "invalid cursor input or query scope" }
func (e *CursorInputError) GoString() string   { return e.Error() }
func (e *CursorInputError) Unwrap() error      { return e.cause }
func (*CursorInputError) Is(target error) bool { return target == fault.Invalid }
func cursorInputFailure(cause error) error     { return &CursorInputError{cause: cause} }

// Validate checks the token's bounded structure without database I/O. Query
// execution still verifies the query fingerprint and every ordered key codec.
func (c Cursor[R]) Validate() error { _, err := decodeCursor(c.token); return err }

// Validate checks size, exclusive directions and supplied token structure.
// Omit both directions for the first page; a supplied zero cursor is invalid.
func (r CursorRequest[R]) Validate() error {
	if !validCursorRequest(r) {
		return cursorInputFailure(invalidCursor())
	}
	for _, optional := range []value.Optional[Cursor[R]]{r.After, r.Before} {
		if cursor, set := optional.Get(); set {
			if err := cursor.Validate(); err != nil {
				return cursorInputFailure(err)
			}
		}
	}
	return nil
}

// Validate checks bounded output metadata without querying for navigation.
// Empty pages carry no cursor; a nonempty page's navigation remains a hint.
func (p CursorPage[R]) Validate() error {
	if !validPageSize(p.Size) || len(p.Items) > p.Size || len(p.Items) == 0 && (p.Next.IsSet() || p.Previous.IsSet()) {
		return fault.New(fault.Invalid, "invalid cursor page metadata")
	}
	for _, optional := range []value.Optional[Cursor[R]]{p.Next, p.Previous} {
		if cursor, set := optional.Get(); set {
			if err := cursor.Validate(); err != nil {
				return err
			}
		}
	}
	return nil
}
