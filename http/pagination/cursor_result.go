package pagination

import (
	"context"
	"slices"

	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/value"
)

// CursorResult retains a source-owned navigation position alongside explicit
// DTO values. MapCursorPage is its constructor; the zero value is invalid.
// Its private metadata preserves the validated ORM result without retaining
// original model rows or manufacturing cursors for the DTO type.
type CursorResult[M, T any] struct {
	items          []T
	size           int
	next, previous value.Optional[query.Cursor[M]]
	defined        bool
}

func (p CursorResult[M, T]) Items() []T                                { return slices.Clone(p.items) }
func (p CursorResult[M, T]) Size() int                                 { return p.size }
func (p CursorResult[M, T]) Next() value.Optional[query.Cursor[M]]     { return p.next }
func (p CursorResult[M, T]) Previous() value.Optional[query.Cursor[M]] { return p.previous }

// MapCursorPage selects DTO values while preserving the source's cursor owner,
// validated size and navigation. It never invokes a getter implicitly. Failure
// returns an invalid zero result and the same callback ownership as MapPage.
func MapCursorPage[M, T any](ctx context.Context, page query.CursorPage[M], mapper func(M) (T, error)) (CursorResult[M, T], error) {
	if err := page.Validate(); err != nil {
		return CursorResult[M, T]{}, err
	}
	items, err := mapItems(ctx, page.Items, mapper)
	if err != nil {
		return CursorResult[M, T]{}, err
	}
	return CursorResult[M, T]{items: items, size: page.Size, next: page.Next, previous: page.Previous, defined: true}, nil
}

// CursorMeta reports page size only; cursor pagination does not count totals.
type CursorMeta struct {
	Size int `json:"per_page"`
}

// CursorResponse contains explicit DTOs and source-owned navigation serialized
// through endpoint URLs. Its data is always an array; empty pages have no links.
type CursorResponse[T any] struct {
	Data  []T        `json:"data"`
	Meta  CursorMeta `json:"meta"`
	Links Links      `json:"links"`
}
