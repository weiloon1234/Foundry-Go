// Package command supplies application-owned notification delivery operations.
// Parse before boot; Run borrows the application's configured manager.
package command

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/notifications"
)

// Command is a validated immutable operation. Resolve is guarded by the state
// the operator observed, so a delivery that moved meanwhile is left unchanged.
type Command struct {
	operation    string
	state        notifications.State
	limit        int
	after        notifications.DeliveryID
	id           notifications.DeliveryID
	resolution   notifications.Resolution
	notification notifications.NotificationID
	json         bool
}

const usage = "notifications deliveries|resolve|deliver [--format text|json]; deliveries: --state running|uncertain [--limit 20] [--after id]; resolve: --id id --expect running|uncertain --as delivered|rejected|resend; deliver: --notification id"

func Parse(args []string, help io.Writer) (Command, error) {
	if help == nil {
		return Command{}, fault.New(fault.Invalid, "notification commands require help output")
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		if _, err := io.WriteString(help, usage+"\n"); err != nil {
			return Command{}, err
		}
		return Command{}, flag.ErrHelp
	}
	if len(args) < 2 || args[0] != "notifications" {
		return Command{}, cli.Usage(usage)
	}
	result := Command{operation: args[1], limit: 20}
	switch result.operation {
	case "deliveries", "resolve", "deliver":
	default:
		return Command{}, cli.Usage(usage)
	}
	flags := flag.NewFlagSet("notifications "+result.operation, flag.ContinueOnError)
	flags.SetOutput(help)
	format := flags.String("format", "text", "text or json")
	undecided := func(text string) (notifications.State, error) {
		state := notifications.State(text)
		if state != notifications.Running && state != notifications.Uncertain {
			return "", fault.New(fault.Invalid, "state must be running or uncertain")
		}
		return state, nil
	}
	switch result.operation {
	case "deliveries":
		flags.Func("state", "running or uncertain", func(text string) (err error) { result.state, err = undecided(text); return err })
		flags.IntVar(&result.limit, "limit", 20, "maximum deliveries (1-100)")
		flags.Func("after", "last delivery ID of the previous page", func(text string) (err error) {
			result.after, err = notifications.ParseDeliveryID(text)
			return err
		})
	case "resolve":
		flags.Func("id", "delivery ID", func(text string) (err error) { result.id, err = notifications.ParseDeliveryID(text); return err })
		flags.Func("expect", "observed state: running or uncertain", func(text string) (err error) { result.state, err = undecided(text); return err })
		flags.Func("as", "delivered, rejected or resend", func(text string) error {
			result.resolution = notifications.Resolution(text)
			switch result.resolution {
			case notifications.ResolveDelivered, notifications.ResolveRejected, notifications.ResolveResend:
				return nil
			}
			return fault.New(fault.Invalid, "resolution must be delivered, rejected or resend")
		})
	case "deliver":
		flags.Func("notification", "notification ID", func(text string) (err error) {
			result.notification, err = model.ParseID[notifications.Notification](text)
			return err
		})
	}
	if err := cli.ParseFlags(flags, args[2:]); err != nil {
		return Command{}, err
	}
	if flags.NArg() != 0 || (*format != "text" && *format != "json") {
		return Command{}, cli.Usage(usage)
	}
	result.json = *format == "json"
	switch {
	case result.operation == "deliveries" && (result.state == "" || result.limit < 1 || result.limit > notifications.MaxDeliveryPage):
		return Command{}, cli.Usage("deliveries requires --state and a --limit between 1 and 100")
	case result.operation == "resolve" && (result.id.Validate() != nil || result.state == "" || result.resolution == ""):
		return Command{}, cli.Usage("resolve requires --id, --expect and --as")
	case result.operation == "deliver" && result.notification.IsZero():
		return Command{}, cli.Usage("deliver requires --notification")
	}
	return result, nil
}

// Declaration registers the notifications command in the ordinary CLI registry.
func Declaration(construct func(foundation.Resolver) (*notifications.Manager, error)) (cli.Declaration, error) {
	if construct == nil {
		return cli.Declaration{}, fault.New(fault.Invalid, "notification commands require a manager constructor")
	}
	definition := cli.Define("notifications", "Inspect and resolve running or uncertain notification deliveries", func(args []string, help io.Writer) (Command, error) {
		if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
			return Parse(args, help)
		}
		return Parse(append([]string{"notifications"}, args...), help)
	})
	return definition.Declare(func(resolver foundation.Resolver) (cli.Handler[Command], error) {
		manager, err := construct(resolver)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, command Command, streams cli.Streams) error {
			return command.Run(ctx, manager, streams.Out)
		}, nil
	})
}

type resolved struct {
	ID      string `json:"id"`
	Changed bool   `json:"changed"`
}

// Run emits operational metadata only: never payloads, routes or rendered content.
func (c Command) Run(ctx context.Context, manager *notifications.Manager, output io.Writer) error {
	if ctx == nil || output == nil || manager == nil {
		return fault.New(fault.Invalid, "notification command requires context, manager and output")
	}
	switch c.operation {
	case "deliveries":
		rows, err := manager.Deliveries(ctx, c.state, c.limit, c.after)
		if err != nil {
			return err
		}
		if rows == nil {
			rows = []notifications.DeliveryInfo{}
		}
		if c.json {
			return json.NewEncoder(output).Encode(struct {
				Deliveries []notifications.DeliveryInfo `json:"deliveries"`
			}{rows})
		}
		for _, row := range rows {
			if _, err := fmt.Fprintf(output, "%s\tnotification=%s\t%s/%s\t%s\tattempts=%d\t%s\n", row.Key, row.Notification.String(), row.Channel, row.Kind, row.State, row.Attempts, row.UpdatedAt.Format(time.RFC3339Nano)); err != nil {
				return err
			}
		}
		return nil
	case "resolve":
		changed, err := manager.ResolveDelivery(ctx, c.id, c.state, c.resolution)
		if err != nil {
			return err
		}
		if c.json {
			return json.NewEncoder(output).Encode(resolved{ID: c.id.String(), Changed: changed})
		}
		_, err = fmt.Fprintf(output, "%s\tchanged=%t\n", c.id.String(), changed)
		return err
	case "deliver":
		statuses, err := manager.Deliver(ctx, c.notification)
		if c.json {
			if encodeErr := json.NewEncoder(output).Encode(struct {
				Channels []notifications.ChannelStatus `json:"channels"`
			}{statuses}); encodeErr != nil && err == nil {
				err = encodeErr
			}
			return err
		}
		for _, status := range statuses {
			if _, writeErr := fmt.Fprintf(output, "%s\t%s\tattempts=%d\n", status.Channel, status.State, status.Attempts); writeErr != nil && err == nil {
				err = writeErr
			}
		}
		return err
	}
	return fault.New(fault.Invalid, "uninitialized notification command; use Parse")
}
