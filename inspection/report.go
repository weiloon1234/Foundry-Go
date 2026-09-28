// Package inspection collects feature-owned declarations and configuration
// provenance. It never resolves services, starts kernels or reads setting values.
package inspection

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/weiloon1234/Foundry-Go/cli"
	"github.com/weiloon1234/Foundry-Go/config"
	"github.com/weiloon1234/Foundry-Go/contract/manifest"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	foundryhttp "github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/schedule"
)

// Sources borrows actual assembled registries or pure declarations. Builder
// inspection runs repeatable Register callbacks, never constructors or Boot.
// Supply either a job registry or its already assembled dispatcher, not both.
type Sources struct {
	Builder       *foundation.Builder
	HTTP          *foundryhttp.Router
	Jobs          *jobs.Registry
	Dispatcher    *jobs.Dispatcher
	Schedules     *schedule.Registry
	Commands      *cli.Registry
	Contracts     *manifest.Manifest
	Configuration []config.Report
}
type Report struct {
	Assembly      foundation.Inspection   `json:"assembly"`
	Routes        []foundryhttp.RouteInfo `json:"routes"`
	Jobs          []jobs.Description      `json:"jobs"`
	Schedules     []schedule.Description  `json:"schedules"`
	Commands      []cli.Description       `json:"commands"`
	Configuration []config.Entry          `json:"configuration"`
	Contracts     *manifest.Document      `json:"contracts,omitempty"`
}

// Collect returns owned snapshots using each feature's existing metadata API.
// It performs no database, queue, lease, HTTP or credential issuance operation.
func Collect(ctx context.Context, sources Sources) (Report, error) {
	var result Report
	if ctx == nil || sources.Jobs != nil && sources.Dispatcher != nil {
		return result, fault.New(fault.Invalid, "inspection requires a context and unambiguous job source")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if sources.Builder != nil {
		assembly, err := sources.Builder.Inspect(ctx)
		if err != nil {
			return Report{}, err
		}
		result.Assembly = assembly
	}
	result.Routes = sources.HTTP.Routes()
	result.Jobs = sources.Jobs.Describe()
	if sources.Dispatcher != nil {
		result.Jobs = sources.Dispatcher.Describe()
	}
	result.Schedules = sources.Schedules.Describe()
	result.Commands = sources.Commands.Describe()
	if sources.Contracts != nil {
		document, err := sources.Contracts.Snapshot()
		if err != nil {
			return Report{}, err
		}
		result.Contracts = &document
	}
	seen := make(map[string]bool)
	for _, report := range sources.Configuration {
		for _, entry := range report.Entries() {
			if seen[entry.Name] {
				return Report{}, fault.New(fault.Duplicate, "configuration provenance name is repeated; retain its namespace")
			}
			seen[entry.Name] = true
			result.Configuration = append(result.Configuration, entry)
		}
	}
	slices.SortFunc(result.Configuration, func(a, b config.Entry) int { return strings.Compare(a.Name, b.Name) })
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	return result, nil
}

type Section string

const (
	All           Section = "all"
	Routes        Section = "routes"
	Jobs          Section = "jobs"
	Schedules     Section = "schedules"
	Plugins       Section = "plugins"
	Commands      Section = "commands"
	Configuration Section = "configuration"
	Contracts     Section = "contracts"
)

type Format string

const (
	JSON Format = "json"
	Text Format = "text"
)

type Arguments struct {
	Section Section
	Format  Format
}

func (a Arguments) Validate() error {
	switch a.Section {
	case All, Routes, Jobs, Schedules, Plugins, Commands, Configuration, Contracts:
	default:
		return cli.Usage("unknown inspection section")
	}
	if a.Format != JSON && a.Format != Text {
		return cli.Usage("inspection format must be json or text")
	}
	return nil
}

// Write selects an existing owned metadata section. Text uses indented JSON so
// typed policy details remain visible; JSON uses compact machine-readable JSON.
// Configuration sections contain provenance names and secret markers, no values.
func Write(ctx context.Context, output io.Writer, report Report, arguments Arguments) error {
	if ctx == nil || output == nil {
		return fault.New(fault.Invalid, "inspection output requires a context and writer")
	}
	if err := arguments.Validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var selected any
	switch arguments.Section {
	case All:
		selected = report
	case Routes:
		selected = report.Routes
	case Jobs:
		selected = report.Jobs
	case Schedules:
		selected = report.Schedules
	case Plugins:
		selected = report.Assembly.Plugins
	case Commands:
		selected = report.Commands
	case Configuration:
		selected = report.Configuration
	case Contracts:
		selected = report.Contracts
	}
	return callback.Isolated("write declaration inspection", func() error {
		encoder := json.NewEncoder(output)
		if arguments.Format == Text {
			encoder.SetIndent("", "  ")
		}
		if err := encoder.Encode(selected); err != nil {
			return err
		}
		return ctx.Err()
	})
}
func (Report) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("application declaration inspection"))
}
