// Package command provides read-only inspection of orphaned model translations.
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
	options, err := extensioncommand.Parse("translations", args, help)
	return Command{options: options}, err
}
func (c Command) Run(ctx context.Context, manager *translations.Manager, out io.Writer) error {
	return extensioncommand.Run(ctx, c.options, out, func(ctx context.Context, owner extensions.OwnerName, cursor translations.Cursor, limit int) (extensionmaintenance.Page[translations.Orphan], error) {
		page, err := translations.InspectOrphans(ctx, manager, owner, cursor, limit)
		return extensionmaintenance.Page[translations.Orphan]{Rows: page.Orphans, Scanned: page.Scanned, Next: page.Next}, err
	}, func(row translations.Orphan) []string {
		return []string{row.Key, string(row.Field), string(row.Locale)}
	})
}
