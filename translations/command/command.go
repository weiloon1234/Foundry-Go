// Package command provides read-only inspection of orphaned model translations
// and re-scoping of stale rows, which writes only with an explicit --apply.
package command

import (
	"context"
	"io"

	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/internal/extensioncommand"
	"github.com/weiloon1234/Foundry-Go/internal/extensionmaintenance"
	"github.com/weiloon1234/Foundry-Go/translations"
)

type Command struct{ options extensioncommand.Options }

func Parse(args []string, help io.Writer) (Command, error) {
	options, err := extensioncommand.ParseMaintenance("translations", args, help)
	return Command{options: options}, err
}
func (c Command) Run(ctx context.Context, manager *translations.Manager, out io.Writer) error {
	if c.options.Undeclared() {
		return extensioncommand.RunUndeclared(ctx, c.options, out, func(ctx context.Context, owner extensions.OwnerName) (extensionmaintenance.Undeclared, error) {
			return translations.InspectUndeclared(ctx, manager, owner)
		})
	}
	if c.options.Rescope() {
		return extensioncommand.RunRescope(ctx, c.options, out, func(ctx context.Context, owner extensions.OwnerName, cursor translations.Cursor, limit int, apply bool) (extensionmaintenance.RescopePage, error) {
			if apply {
				return translations.Rescope(ctx, manager, owner, cursor, limit)
			}
			return translations.InspectStale(ctx, manager, owner, cursor, limit)
		})
	}
	return extensioncommand.Run(ctx, c.options, out, func(ctx context.Context, owner extensions.OwnerName, cursor translations.Cursor, limit int) (extensionmaintenance.Page[translations.Orphan], error) {
		page, err := translations.InspectOrphans(ctx, manager, owner, cursor, limit)
		return extensionmaintenance.Page[translations.Orphan]{Rows: page.Orphans, Scanned: page.Scanned, Next: page.Next}, err
	}, func(row translations.Orphan) []string {
		return []string{row.Key, string(row.Field), string(row.Locale)}
	})
}
