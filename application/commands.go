package application

import (
	"cmp"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/internal/clidiag"
	"maps"
	"net/netip"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"time"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/frameworkinfo"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/maintenance"
)

// DownArguments are the parsed `down` flags.
type DownArguments struct {
	Format     string
	Retry      time.Duration
	Message    string
	Secret     string
	WithSecret bool
	Allow      []netip.Prefix
	Except     []maintenance.Rule
}

type maintenanceArguments struct{ Format string }

type maintenanceResult struct {
	Down       bool   `json:"down"`
	BypassPath string `json:"bypass_path,omitempty"`
	Since      string `json:"since,omitempty"`
}

func maintenanceFormat(format string) error {
	if format != "text" && format != "json" {
		return clidiag.Usage("format must be text or json")
	}
	return nil
}

var downCommand = cli.Define("down", "Put every instance sharing the maintenance store into maintenance mode", cli.Flags(func(flags *flag.FlagSet, args *DownArguments) {
	flags.StringVar(&args.Format, "format", "text", "text or json")
	flags.Func("retry", "Retry-After seconds advertised to clients (0-86400)", func(value string) error {
		seconds, err := strconv.Atoi(value)
		if err != nil || seconds < 0 || time.Duration(seconds)*time.Second > maintenance.MaxRetryAfter {
			return safeerror.NewFault(fault.Invalid, "retry must be whole seconds up to one day")
		}
		args.Retry = time.Duration(seconds) * time.Second
		return nil
	})
	flags.StringVar(&args.Message, "message", "", "public message rendered in maintenance responses")
	flags.StringVar(&args.Secret, "secret", "", "bypass secret; visiting /<secret> sets a bypass cookie")
	flags.BoolVar(&args.WithSecret, "with-secret", false, "generate and print a bypass secret")
	flags.Func("allow", "admit an IP address or CIDR while down (repeatable)", func(value string) error {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			address, addressErr := netip.ParseAddr(value)
			if addressErr != nil {
				return safeerror.NewFault(fault.Invalid, "allow requires an IP address or CIDR")
			}
			address = address.Unmap().WithZone("")
			prefix = netip.PrefixFrom(address, address.BitLen())
		}
		args.Allow = append(args.Allow, prefix.Masked())
		return nil
	})
	flags.Func("except", `exempt "[METHOD ]/path[/*]" while down (repeatable)`, func(value string) error {
		rule, err := maintenance.ParseRule(value)
		if err == nil {
			args.Except = append(args.Except, rule)
		}
		return err
	})
}, func(args DownArguments) error {
	if args.Secret != "" && args.WithSecret {
		return clidiag.Usage("use either --secret or --with-secret")
	}
	return maintenanceFormat(args.Format)
})).AllowDuringMaintenance()

var maintenanceFlags = cli.Flags(func(flags *flag.FlagSet, args *maintenanceArguments) {
	flags.StringVar(&args.Format, "format", "text", "text or json")
}, func(args maintenanceArguments) error {
	return maintenanceFormat(args.Format)
})

var upCommand = cli.Define("up", "Bring every instance sharing the maintenance store out of maintenance mode", maintenanceFlags).AllowDuringMaintenance()
var statusCommand = cli.Define("maintenance:status", "Read maintenance status from the shared store", maintenanceFlags).AllowDuringMaintenance()

// MaintenanceCommands returns `down`, `up`, and `maintenance:status`, in that order.
// They run while maintenance is paused and use the configured shared store, which
// every instance polls; without Maintenance.Store they fail with fault.Missing.
// Register them in the application's CLI registry and run them through the
// ordinary CLI kernel.
func MaintenanceCommands() ([]cli.Declaration, error) {
	resolveStore := func(r foundation.Resolver) (maintenance.Store, error) {
		store, err := foundation.Resolve(r, MaintenanceStoreKey)
		if err != nil {
			return nil, safeerror.WrapFault(fault.Missing, "maintenance commands require a configured maintenance store", err)
		}
		return store, nil
	}
	resolve := func(r foundation.Resolver) (*maintenance.Gate, maintenance.Store, error) {
		gate, err := foundation.Resolve(r, MaintenanceKey)
		if err != nil {
			return nil, nil, err
		}
		store, err := resolveStore(r)
		return gate, store, err
	}
	down, err := downCommand.Declare(func(r foundation.Resolver) (cli.Handler[DownArguments], error) {
		gate, store, err := resolve(r)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, args DownArguments, streams cli.Streams) error {
			state := maintenance.State{Down: true, RetryAfter: args.Retry, Message: args.Message, Allow: args.Allow, Exempt: args.Except, Since: time.Now().UTC()}
			secret, err := args.Secret, error(nil)
			if args.WithSecret {
				if secret, err = maintenance.NewSecret(); err != nil {
					return err
				}
			}
			if secret != "" {
				if state.Secret, err = maintenance.DigestSecret(secret); err != nil {
					return cli.InvalidArguments(err)
				}
			}
			if err := maintenance.Publish(ctx, gate, store, state); err != nil {
				return err
			}
			if args.Format == "json" {
				result := maintenanceResult{Down: true}
				if args.WithSecret {
					result.BypassPath = "/" + secret
				}
				return json.NewEncoder(streams.Out).Encode(result)
			}
			if _, err := fmt.Fprintln(streams.Out, "Maintenance mode enabled."); err != nil {
				return err
			}
			if args.WithSecret {
				_, err = fmt.Fprintf(streams.Out, "Bypass: visit /%s to receive the bypass cookie.\n", secret)
			}
			return err
		}, nil
	})
	if err != nil {
		return nil, err
	}
	up, err := upCommand.Declare(func(r foundation.Resolver) (cli.Handler[maintenanceArguments], error) {
		gate, store, err := resolve(r)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, args maintenanceArguments, streams cli.Streams) error {
			if err := maintenance.Publish(ctx, gate, store, maintenance.State{}); err != nil {
				return err
			}
			if args.Format == "json" {
				return json.NewEncoder(streams.Out).Encode(maintenanceResult{Down: false})
			}
			_, err := fmt.Fprintln(streams.Out, "Maintenance mode disabled.")
			return err
		}, nil
	})
	if err != nil {
		return nil, err
	}
	status, err := statusCommand.Declare(func(r foundation.Resolver) (cli.Handler[maintenanceArguments], error) {
		store, err := resolveStore(r)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, args maintenanceArguments, streams cli.Streams) error {
			state, exists, err := store.Load(ctx)
			if err != nil {
				return err
			}
			result := maintenanceResult{Down: exists && state.Down}
			if result.Down && !state.Since.IsZero() {
				result.Since = state.Since.UTC().Format(time.RFC3339)
			}
			if args.Format == "json" {
				return json.NewEncoder(streams.Out).Encode(result)
			}
			if result.Down {
				_, err = fmt.Fprintf(streams.Out, "Application is down since %s\n", state.Since.UTC().Format(time.RFC3339))
			} else {
				_, err = fmt.Fprintln(streams.Out, "Application is up")
			}
			return err
		}, nil
	})
	if err != nil {
		return nil, err
	}
	return []cli.Declaration{down, up, status}, nil
}

// About is the secret-free application summary printed by AboutCommand.
type About struct {
	Framework   string            `json:"framework"`
	Go          string            `json:"go"`
	Platform    string            `json:"platform"`
	Application string            `json:"application"`
	Environment string            `json:"environment"`
	TimeZone    string            `json:"time_zone"`
	Kernels     map[string]string `json:"kernels"`
	Maintenance string            `json:"maintenance"`
	Services    map[string]string `json:"services"`
}

type aboutArguments struct{ JSON bool }

// AboutCommand describes the framework version, Go toolchain and the configured
// kernels/service drivers from settings. It prints names and drivers only:
// hosts, users, credentials and paths are never included. It performs no I/O
// beyond writing output, so it can run before Build.
func AboutCommand(name cli.Name, settings Settings) (cli.Declaration, error) {
	command := cli.Define(name, "Describe the framework version, runtime and configured drivers", cli.Flags(func(flags *flag.FlagSet, args *aboutArguments) {
		flags.BoolVar(&args.JSON, "json", false, "write JSON instead of a table")
	}, nil))
	about := describe(settings)
	return command.Declare(func(foundation.Resolver) (cli.Handler[aboutArguments], error) {
		return func(ctx context.Context, args aboutArguments, streams cli.Streams) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if args.JSON {
				encoder := json.NewEncoder(streams.Out)
				encoder.SetIndent("", "  ")
				return encoder.Encode(about)
			}
			rows := [][]string{{"Framework", about.Framework}, {"Go", about.Go}, {"Platform", about.Platform}, {"Application", about.Application}, {"Environment", about.Environment}, {"Time zone", about.TimeZone}, {"Maintenance", about.Maintenance}}
			for _, key := range slices.Sorted(maps.Keys(about.Kernels)) {
				rows = append(rows, []string{"Kernel " + key, about.Kernels[key]})
			}
			for _, key := range slices.Sorted(maps.Keys(about.Services)) {
				rows = append(rows, []string{key, about.Services[key]})
			}
			return cli.WriteTable(streams.Out, []string{"Item", "Value"}, rows)
		}, nil
	})
}

func frameworkVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	if info.Main.Path == frameworkinfo.ModulePath {
		return cmp.Or(info.Main.Version, "(devel)")
	}
	for _, dependency := range info.Deps {
		if dependency.Path == frameworkinfo.ModulePath {
			if dependency.Replace != nil {
				return cmp.Or(dependency.Version, "(devel)") + " (replaced)"
			}
			return dependency.Version
		}
	}
	return "unknown"
}

func describe(s Settings) About {
	enabled := func(on bool, detail string) string {
		if !on {
			return "disabled"
		}
		return cmp.Or(detail, "enabled")
	}
	about := About{Framework: frameworkVersion(), Go: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH, Application: s.Services.Namespace.Application, Environment: s.Services.Namespace.Environment, TimeZone: string(s.TimeZone),
		Kernels:     map[string]string{"http": enabled(s.HTTP.Enabled, s.HTTP.Server.Address), "worker": enabled(s.Worker.Enabled, ""), "scheduler": enabled(s.Scheduler.Enabled, ""), "realtime": enabled(s.Realtime.Enabled, s.Realtime.Path)},
		Maintenance: "local", Services: make(map[string]string)}
	switch {
	case s.Realtime.Enabled && s.Realtime.Shared:
		about.Kernels["realtime"] = "shared " + s.Realtime.Path
	case s.Realtime.Publisher:
		about.Kernels["realtime"] = "publisher"
	}
	if s.Maintenance.Store != "" {
		about.Maintenance = "cache store " + string(s.Maintenance.Store)
	}
	add := func(family, name, driver string) { about.Services[family+" "+name] = driver }
	for name, connection := range s.Services.Database.Connections {
		driver := "postgres"
		if connection.ReadEnabled {
			driver += " (primary+read)"
		}
		add("database", string(name), driver)
	}
	for name := range s.Services.Redis.Connections {
		add("redis", string(name), "redis")
	}
	for name, store := range s.Services.Cache.Stores {
		add("cache", string(name), string(store.Driver))
	}
	for name, disk := range s.Services.Storage.Disks {
		add("disk", string(name), string(disk.Driver))
	}
	for name, mailer := range s.Services.Mail.Mailers {
		add("mailer", string(name), string(mailer.Driver))
	}
	for name, connection := range s.Services.Jobs.Connections {
		add("jobs", string(name), string(connection.Driver))
	}
	for name, channel := range s.Log.Channels {
		add("log", string(name), string(channel.Sink.Driver))
	}
	return about
}
