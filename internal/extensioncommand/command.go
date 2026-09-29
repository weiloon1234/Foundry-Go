// Package extensioncommand shares the model-extension maintenance command
// grammar and bounded page output. Orphan inspection is read-only; re-scoping
// writes only with an explicit --apply. Feature commands borrow managers.
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
	rescope  bool
	apply    bool
}

// Rescope reports whether the parsed command is the re-scope maintenance form.
func (o Options) Rescope() bool { return o.rescope }

// Parse accepts only the read-only orphan inspection form.
func Parse(name string, args []string, help io.Writer) (Options, error) {
	return parse(name, args, help, false)
}

// ParseMaintenance also accepts the re-scope form for features that support it.
func ParseMaintenance(name string, args []string, help io.Writer) (Options, error) {
	return parse(name, args, help, true)
}
func parse(name string, args []string, help io.Writer, maintenance bool) (Options, error) {
	usage := "usage: " + name + " orphans --owner name [--page-size 100] [--format text|json]"
	if maintenance {
		usage += "\n       " + name + " rescope --owner name [--page-size 100] [--format text|json] [--apply]"
	}
	if help == nil || !identifier.Semantic(name) {
		return Options{}, fault.New(fault.Invalid, usage)
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		_, err := io.WriteString(help, usage+"\n")
		return Options{}, errors.Join(flag.ErrHelp, err)
	}
	if len(args) < 2 || args[0] != name || args[1] != "orphans" && (!maintenance || args[1] != "rescope") {
		return Options{}, fault.New(fault.Invalid, usage)
	}
	rescope := args[1] == "rescope"
	flags := flag.NewFlagSet(name+" "+args[1], flag.ContinueOnError)
	flags.SetOutput(help)
	owner := flags.String("owner", "", "registered model owner")
	size := flags.Int("page-size", 100, "rows inspected per query")
	format := flags.String("format", "text", "text or json")
	var apply *bool
	if rescope {
		apply = flags.Bool("apply", false, "move stale rows into the owner's current scope")
	}
	if err := flags.Parse(args[2:]); err != nil {
		return Options{}, err
	}
	if flags.NArg() != 0 || !identifier.Semantic(*owner) || *size < 1 || *size > 1000 || *format != "text" && *format != "json" {
		return Options{}, fault.New(fault.Invalid, usage)
	}
	return Options{owner: extensions.OwnerName(*owner), pageSize: *size, json: *format == "json", rescope: rescope, apply: apply != nil && *apply}, nil
}

// RunRescope pages through stale rows, moving them only for --apply. Output
// contains opaque row keys only; rows left in place are listed with a
// "conflict", "missing" (subject no longer exists) or "undeclared" (recorded
// under a model the owner does not declare) marker.
func RunRescope(ctx context.Context, options Options, out io.Writer, rescope func(context.Context, extensions.OwnerName, extensionmaintenance.Cursor, int, bool) (extensionmaintenance.RescopePage, error)) error {
	if ctx == nil || out == nil || options.owner == "" || options.pageSize < 1 || !options.rescope || rescope == nil {
		return fault.New(fault.Invalid, "uninitialized model extension re-scope command")
	}
	return callback.Isolated("model extension re-scope command", func() error {
		var cursor extensionmaintenance.Cursor
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			page, err := rescope(ctx, options.owner, cursor, options.pageSize, options.apply)
			if err != nil {
				return err
			}
			if options.json {
				err = json.NewEncoder(out).Encode(struct {
					Applied    bool                         `json:"applied"`
					Rows       []extensionmaintenance.Stale `json:"rows"`
					Conflicts  []string                     `json:"conflicts"`
					Missing    []string                     `json:"missing"`
					Undeclared []string                     `json:"undeclared"`
				}{options.apply, page.Rows, page.Conflicts, page.Missing, page.Undeclared})
			} else {
				for _, row := range page.Rows {
					if _, err = fmt.Fprintf(out, "%s\t%s\n", row.Key, row.Target); err != nil {
						break
					}
				}
				for _, left := range []struct {
					keys   []string
					marker string
				}{{page.Conflicts, "conflict"}, {page.Missing, "missing"}, {page.Undeclared, "undeclared"}} {
					for _, key := range left.keys {
						if err != nil {
							break
						}
						_, err = fmt.Fprintf(out, "%s\t%s\n", key, left.marker)
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
func Run[R any](ctx context.Context, options Options, out io.Writer, inspect func(context.Context, extensions.OwnerName, extensionmaintenance.Cursor, int) (extensionmaintenance.Page[R], error), columns func(R) []string) error {
	if ctx == nil || out == nil || options.owner == "" || options.pageSize < 1 || options.rescope || inspect == nil || columns == nil {
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
