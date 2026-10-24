package querycore

import (
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Cursor is an opaque model/result-owned position. Tokens are versioned and scoped to
// the query but are not signed/encrypted credentials. Apply authorization on
// every request. Token explicitly exports the transport value; formatting omits it.
type Cursor[M any] struct {
	_     [0]*M
	token string
}

func (c Cursor[M]) Token() string { return c.token }

func (c Cursor[M]) String() string { return "[cursor]" }

func (c Cursor[M]) GoString() string { return c.String() }

// ParseCursor validates bounded transport structure. CursorPaginate additionally
// verifies the model/query scope and decodes every key through its field codec.
func ParseCursor[M any](token string) (Cursor[M], error) {
	if _, err := DecodeCursor(token); err != nil {
		return Cursor[M]{}, err
	}
	return Cursor[M]{token: token}, nil
}

// CursorRequest selects a bounded forward or backward page in the query's
// declared order. After and Before are mutually exclusive; omit both to start.
type CursorRequest[M any] struct {
	Size          int
	After, Before value.Optional[Cursor[M]]
}

// CursorPage keeps canonical query order even for backward traversal. Next and
// Previous are navigation hints, not guarantees against concurrent data changes.
type CursorPage[M any] struct {
	Items          []M
	Size           int
	Next, Previous value.Optional[Cursor[M]]
}

// Validate checks the token's bounded structure without database I/O. Query
// execution still verifies the query fingerprint and every ordered key codec.
func (c Cursor[R]) Validate() error { _, err := DecodeCursor(c.token); return err }

// Validate checks size, exclusive directions and supplied token structure.
// Omit both directions for the first page; a supplied zero cursor is invalid.
func (r CursorRequest[R]) Validate() error {
	if !ValidCursorRequest(r.Size, r.After.IsSet(), r.Before.IsSet()) {
		return CursorInputFailure(InvalidCursor())
	}
	for _, optional := range []value.Optional[Cursor[R]]{r.After, r.Before} {
		if cursor, set := optional.Get(); set {
			if err := cursor.Validate(); err != nil {
				return CursorInputFailure(err)
			}
		}
	}
	return nil
}

// Validate checks bounded output metadata without querying for navigation.
// Empty pages carry no cursor; a nonempty page's navigation remains a hint.
func (p CursorPage[R]) Validate() error {
	if !ValidPageSize(p.Size) || len(p.Items) > p.Size || len(p.Items) == 0 && (p.Next.IsSet() || p.Previous.IsSet()) {
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

// ValidCursorRequest checks the request shape before decoding its boundary.
func ValidCursorRequest(size int, after, before bool) bool {
	return ValidPageSize(size) && !(after && before)
}
