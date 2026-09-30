// Package command provides metadata maintenance commands, borrowing already
// assembled services. Orphan inspection is read-only and exposes no deletion
// flag; re-scoping only lists stale rows unless --apply is given.
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
	options, err := extensioncommand.ParseMaintenance("metadata", args, help)
	return Command{options: options}, err
}

// Run emits only opaque row keys and key names, never owner/value payloads.
// JSON output is one bounded page per line.
func (c Command) Run(ctx context.Context, manager *metadata.Manager, out io.Writer) error {
	if c.options.Undeclared() {
		return extensioncommand.RunUndeclared(ctx, c.options, out, func(ctx context.Context, owner extensions.OwnerName) (extensionmaintenance.Undeclared, error) {
			return metadata.InspectUndeclared(ctx, manager, owner)
		})
	}
	if c.options.Rescope() {
		return extensioncommand.RunRescope(ctx, c.options, out, func(ctx context.Context, owner extensions.OwnerName, cursor metadata.Cursor, limit int, apply bool) (extensionmaintenance.RescopePage, error) {
			if apply {
				return metadata.Rescope(ctx, manager, owner, cursor, limit)
			}
			return metadata.InspectStale(ctx, manager, owner, cursor, limit)
		})
	}
	return extensioncommand.Run(ctx, c.options, out, func(ctx context.Context, owner extensions.OwnerName, cursor metadata.Cursor, limit int) (extensionmaintenance.Page[metadata.Orphan], error) {
		page, err := metadata.InspectOrphans(ctx, manager, owner, cursor, limit)
		return extensionmaintenance.Page[metadata.Orphan]{Rows: page.Orphans, Scanned: page.Scanned, Next: page.Next}, err
	}, func(row metadata.Orphan) []string { return []string{row.Key, string(row.Name)} })
}
