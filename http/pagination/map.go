package pagination

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// MapPage converts complete source rows into explicitly chosen DTO values while
// preserving the ORM's page metadata. The mapper selects stored fields/getters;
// no persisted model is automatically exposed. Failure returns a zero page.
func MapPage[From, To any](ctx context.Context, page orm.PageResult[From], mapper func(From) (To, error)) (orm.PageResult[To], error) {
	if err := page.Validate(); err != nil {
		return orm.PageResult[To]{}, err
	}
	items, err := mapItems(ctx, page.Items, mapper)
	if err != nil {
		return orm.PageResult[To]{}, err
	}
	return orm.PageResult[To]{Items: items, Number: page.Number, Size: page.Size, Total: page.Total, Pages: page.Pages}, nil
}

// MapSimplePage retains count-free metadata. It never adds a total or page count.
func MapSimplePage[From, To any](ctx context.Context, page orm.SimplePage[From], mapper func(From) (To, error)) (orm.SimplePage[To], error) {
	if err := page.Validate(); err != nil {
		return orm.SimplePage[To]{}, err
	}
	items, err := mapItems(ctx, page.Items, mapper)
	if err != nil {
		return orm.SimplePage[To]{}, err
	}
	return orm.SimplePage[To]{Items: items, Number: page.Number, Size: page.Size, HasMore: page.HasMore}, nil
}

func mapItems[From, To any](ctx context.Context, source []From, mapper func(From) (To, error)) ([]To, error) {
	if ctx == nil || mapper == nil {
		return nil, safeerror.NewFault(fault.Invalid, "page mapping requires a context and mapper")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	items := make([]To, 0, len(source))
	callbackErr := callback.Isolated("map page items", func() error {
		for _, row := range source {
			if err := ctx.Err(); err != nil {
				return err
			}
			item, err := mapper(row)
			if err != nil {
				return err
			}
			items = append(items, item)
		}
		return nil
	})
	if callbackErr != nil {
		return nil, callbackErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
