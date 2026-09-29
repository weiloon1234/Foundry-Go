// Package command provides read-only attachment owner-orphan inspection and
// bounded regeneration of declared image variants.
package command

import (
	"context"
	"io"

	"github.com/weiloon1234/Foundry-Go/attachments"
	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/internal/extensioncommand"
	"github.com/weiloon1234/Foundry-Go/internal/extensionmaintenance"
)

type Command struct{ options extensioncommand.Options }

func Parse(args []string, help io.Writer) (Command, error) {
	options, err := extensioncommand.Parse("attachments", args, help)
	return Command{options: options}, err
}
func (c Command) Run(ctx context.Context, manager *attachments.Manager, out io.Writer) error {
	return extensioncommand.Run(ctx, c.options, out, func(ctx context.Context, owner extensions.OwnerName, cursor attachments.Cursor, limit int) (extensionmaintenance.Page[attachments.Orphan], error) {
		page, err := manager.InspectOrphans(ctx, owner, cursor, limit)
		return extensionmaintenance.Page[attachments.Orphan]{Rows: page.Orphans, Scanned: page.Scanned, Next: page.Next}, err
	}, func(row attachments.Orphan) []string {
		return []string{row.Operation.String(), string(row.Collection), string(row.Locale), string(row.State)}
	})
}
