// Package extensioncommand shares the read-only model-extension orphan command
// grammar and bounded page output. Feature commands borrow assembled managers.
package extensioncommand

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/weiloon1234/Foundry-Go/extensions"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/extensionmaintenance"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

type Options struct {
	owner    extensions.OwnerName
	pageSize int
	json     bool
}

func Parse(name string, args []string, help io.Writer) (Options, error) {
	usage := "usage: " + name + " orphans --owner name [--page-size 100] [--format text|json]"
	if help == nil || !identifier.Semantic(name) {
		return Options{}, fault.New(fault.Invalid, usage)
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		_, err := io.WriteString(help, usage+"\n")
		return Options{}, errors.Join(flag.ErrHelp, err)
	}
	if len(args) < 2 || args[0] != name || args[1] != "orphans" {
		return Options{}, fault.New(fault.Invalid, usage)
	}
	flags := flag.NewFlagSet(name+" orphans", flag.ContinueOnError)
	flags.SetOutput(help)
	owner := flags.String("owner", "", "registered model owner")
	size := flags.Int("page-size", 100, "rows inspected per query")
	format := flags.String("format", "text", "text or json")
	if err := flags.Parse(args[2:]); err != nil {
		return Options{}, err
	}
	if flags.NArg() != 0 || !identifier.Semantic(*owner) || *size < 1 || *size > 1000 || *format != "text" && *format != "json" {
		return Options{}, fault.New(fault.Invalid, usage)
	}
	return Options{owner: extensions.OwnerName(*owner), pageSize: *size, json: *format == "json"}, nil
}
func Run[R any](ctx context.Context, options Options, out io.Writer, inspect func(context.Context, extensions.OwnerName, extensionmaintenance.Cursor, int) (extensionmaintenance.Page[R], error), columns func(R) []string) error {
	if ctx == nil || out == nil || options.owner == "" || options.pageSize < 1 || inspect == nil || columns == nil {
		return fault.New(fault.Invalid, "uninitialized model extension inspection command")
	}
	return callback.Isolated("model extension inspection command", func() error {
		var cursor extensionmaintenance.Cursor
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			page, err := inspect(ctx, options.owner, cursor, options.pageSize)
			if err != nil {
				return err
			}
			if options.json {
				err = json.NewEncoder(out).Encode(struct {
					Scanned int `json:"scanned"`
					Orphans []R `json:"orphans"`
				}{page.Scanned, page.Rows})
			} else {
				for _, row := range page.Rows {
					for i, column := range columns(row) {
						if i > 0 {
							if _, err = io.WriteString(out, "\t"); err != nil {
								break
							}
						}
						if _, err = io.WriteString(out, column); err != nil {
							break
						}
					}
					if err != nil {
						break
					}
					if _, err = fmt.Fprintln(out); err != nil {
						break
					}
				}
			}
			if err != nil {
				return err
			}
			if page.Next.IsZero() {
				return nil
			}
			cursor = page.Next
		}
	})
}
