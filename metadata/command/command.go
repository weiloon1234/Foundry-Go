// Package command provides read-only metadata orphan inspection, borrowing
// already assembled services and exposing no destructive command flag.
package command

import (
	"context"
	"io"

	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/internal/extensioncommand"
	"github.com/weiloon1234/Foundry-Go/internal/extensionmaintenance"
	"github.com/weiloon1234/Foundry-Go/metadata"
)

type Command struct{ options extensioncommand.Options }

func Parse(args []string, help io.Writer) (Command, error) {
	options, err := extensioncommand.Parse("metadata", args, help)
	return Command{options: options}, err
}

// Run emits only opaque row keys and key names, never owner/value payloads.
// JSON output is one bounded page per line.
func (c Command) Run(ctx context.Context, manager *metadata.Manager, out io.Writer) error {
	return extensioncommand.Run(ctx, c.options, out, func(ctx context.Context, owner extensions.OwnerName, cursor metadata.Cursor, limit int) (extensionmaintenance.Page[metadata.Orphan], error) {
		page, err := metadata.InspectOrphans(ctx, manager, owner, cursor, limit)
		return extensionmaintenance.Page[metadata.Orphan]{Rows: page.Orphans, Scanned: page.Scanned, Next: page.Next}, err
	}, func(row metadata.Orphan) []string { return []string{row.Key, string(row.Name)} })
}
