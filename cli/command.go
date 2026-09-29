// Package cli declares typed commands, parses invocations before service startup
// and runs them through Foundry's shared application lifecycle. It never changes
// process globals, installs tools or calls os.Exit.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/maintenance"
	"github.com/weiloon1234/Foundry-Go/observability"
)

type Name string

const MaxCommands = 1024
const MaxArguments = 256
const MaxArgumentBytes = 1 << 20

// DuringMaintenance is the explicit operator flag, placed before the command
// name, that runs one invocation while application maintenance is paused.
// Draining (shutdown) still rejects every invocation.
const DuringMaintenance = "--during-maintenance"

// Decoder performs pure argument parsing. It must return an owned value and
// leave resource acquisition to the command handler. Help returns flag.ErrHelp.
type Decoder[A any] func([]string, io.Writer) (A, error)
type Handler[A any] func(context.Context, A, Streams) error
type Streams struct {
	In       io.Reader
	Out, Err io.Writer
}

func (s Streams) Validate() error {
	if s.In == nil || s.Out == nil || s.Err == nil {
		return fault.New(fault.Invalid, "CLI requires explicit input, output and error streams")
	}
	return nil
}

type Description struct {
	Name      Name   `json:"name"`
	Summary   string `json:"summary"`
	Arguments string `json:"arguments"`
	// Maintenance reports that the command runs while maintenance is paused.
	Maintenance bool `json:"maintenance,omitempty"`
}
type Command[A any] struct {
	description Description
	decode      Decoder[A]
}

// Define keeps the concrete argument type through parsing and handler binding.
// Names are semantic identifiers; subcommands may use a typed custom decoder.
func Define[A any](name Name, summary string, decode Decoder[A]) Command[A] {
	return Command[A]{description: Description{Name: name, Summary: summary, Arguments: reflect.TypeFor[A]().String()}, decode: decode}
}
func (c Command[A]) Name() Name { return c.description.Name }

// AllowDuringMaintenance declares an operational command, such as a migration
// or the maintenance toggles themselves, that runs while maintenance is paused.
// Draining (shutdown) still rejects it. Other commands require the explicit
// DuringMaintenance flag for one invocation.
func (c Command[A]) AllowDuringMaintenance() Command[A] {
	c.description.Maintenance = true
	return c
}
func (c Command[A]) Validate() error {
	if !identifier.Semantic(string(c.Name())) || len(c.description.Summary) > 4096 || !utf8.ValidString(c.description.Summary) || strings.IndexFunc(c.description.Summary, unicode.IsControl) >= 0 || c.decode == nil {
		return fault.New(fault.Invalid, "command requires a semantic name, single-line summary and typed decoder")
	}
	return nil
}

// Declaration is the heterogeneous registration boundary. Factories resolve
// concrete services only for the selected command, after application startup.
// Plugins can export declarations for explicit inclusion in the same registry.
type Declaration struct {
	description Description
	parse       func([]string, io.Writer) (func(context.Context, foundation.Resolver, Streams) error, error)
}

func (c Command[A]) Declare(construct func(foundation.Resolver) (Handler[A], error)) (Declaration, error) {
	if err := c.Validate(); err != nil {
		return Declaration{}, err
	}
	if construct == nil {
		return Declaration{}, fault.New(fault.Invalid, "command requires a typed handler constructor")
	}
	return Declaration{description: c.description, parse: func(args []string, help io.Writer) (func(context.Context, foundation.Resolver, Streams) error, error) {
		input, err := c.decode(args, help)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, resolver foundation.Resolver, streams Streams) error {
			handler, err := construct(resolver)
			if err != nil {
				return err
			}
			if handler == nil {
				return fault.New(fault.Invalid, "command constructor returned a nil handler")
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			return handler(ctx, input, streams)
		}, nil
	}}, nil
}
func (Declaration) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("CLI declaration")) }

// Registry freezes explicit declarations. New, Describe and Parse perform no
// service construction, kernel startup or external I/O beyond the help writer.
type Registry struct{ entries map[Name]Declaration }

func New(declarations ...Declaration) (*Registry, error) {
	if len(declarations) > MaxCommands {
		return nil, fault.New(fault.Invalid, "too many CLI commands")
	}
	r := &Registry{entries: make(map[Name]Declaration, len(declarations))}
	for _, d := range declarations {
		if d.parse == nil || !identifier.Semantic(string(d.description.Name)) {
			return nil, fault.New(fault.Invalid, "invalid CLI declaration")
		}
		if _, exists := r.entries[d.description.Name]; exists {
			return nil, fault.New(fault.Duplicate, "duplicate CLI command")
		}
		r.entries[d.description.Name] = d
	}
	return r, nil
}
func (r *Registry) Describe() []Description {
	if r == nil {
		return nil
	}
	result := make([]Description, 0, len(r.entries))
	for _, d := range r.entries {
		result = append(result, d.description)
	}
	slices.SortFunc(result, func(a, b Description) int { return strings.Compare(string(a.Name), string(b.Name)) })
	return result
}

func validateArguments(args []string) error {
	if len(args) > MaxArguments {
		return Usage("too many command arguments")
	}
	size := 0
	for _, arg := range args {
		if len(arg) > MaxArgumentBytes-size || !utf8.ValidString(arg) || strings.ContainsRune(arg, 0) {
			return Usage("command arguments exceed their bounds or contain invalid text")
		}
		size += len(arg)
	}
	return nil
}
func (r *Registry) Parse(args []string, help io.Writer) (Invocation, error) {
	if r == nil || r.entries == nil || help == nil {
		return Invocation{}, fault.New(fault.Invalid, "CLI parsing requires a registry and help writer")
	}
	if err := validateArguments(args); err != nil {
		return Invocation{}, err
	}
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		var writeErr error
		if err := callback.Isolated("write CLI help", func() error {
			for _, description := range r.Describe() {
				if _, err := fmt.Fprintf(help, "%s\t%s\n", description.Name, description.Summary); err != nil {
					writeErr = err
					break
				}
			}
			return nil
		}); err != nil {
			return Invocation{}, err
		}
		if writeErr != nil {
			return Invocation{}, writeErr
		}
		return Invocation{}, flag.ErrHelp
	}
	bypass := len(args) > 0 && args[0] == DuringMaintenance
	if bypass {
		args = args[1:]
	}
	if len(args) == 0 {
		return Invocation{}, Usage("expected a command; use --help")
	}
	d, ok := r.entries[Name(args[0])]
	if !ok {
		return Invocation{}, Usage("unknown command; use --help")
	}
	var invoke func(context.Context, foundation.Resolver, Streams) error
	var parseErr error
	err := callback.Isolated("parse CLI arguments", func() error { invoke, parseErr = d.parse(slices.Clone(args[1:]), help); return nil })
	if err != nil {
		return Invocation{}, err
	}
	if parseErr != nil {
		return Invocation{}, parseErr
	}
	if invoke == nil {
		return Invocation{}, fault.New(fault.Internal, "command parser returned no invocation")
	}
	return Invocation{state: &invocation{registry: r, name: d.description.Name, maintenance: bypass || d.description.Maintenance, run: invoke}}, nil
}

type invocation struct {
	registry    *Registry
	name        Name
	maintenance bool
	claimed     atomic.Bool
	run         func(context.Context, foundation.Resolver, Streams) error
}

// Invocation owns one parsed command. Copies share its execution claim: an
// invocation runs at most once, including on failure, avoiding accidental retry
// of a partially completed mutation. Parse again for an intentional new run.
type Invocation struct{ state *invocation }

func (i Invocation) Name() Name {
	if i.state == nil {
		return ""
	}
	return i.state.name
}
func (Invocation) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("parsed CLI invocation"))
}
func (i Invocation) Run(ctx context.Context, resolver foundation.Resolver, streams Streams) error {
	if i.state == nil || i.state.run == nil || ctx == nil {
		return fault.New(fault.Invalid, "uninitialized CLI invocation or context")
	}
	if err := streams.Validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !i.state.claimed.CompareAndSwap(false, true) {
		return fault.New(fault.Closed, "CLI invocation already ran")
	}
	return observability.Observe(ctx, observability.Operation{Kind: observability.CLI, Name: observability.Name(i.Name())}, func(ctx context.Context) error {
		gate := maintenance.FromContext(ctx)
		if gate == nil {
			// Manual compositions may attach only an observation recorder.
			gate = observability.FromContext(ctx).Gate()
		}
		if err := gate.Admit(); err != nil && (!i.state.maintenance || err != maintenance.ErrMaintenance) {
			return err
		}
		var outcome error
		if err := callback.Isolated("run CLI command", func() error { outcome = i.state.run(ctx, resolver, streams); return nil }); err != nil {
			return err
		}
		if outcome != nil {
			return outcome
		}
		return ctx.Err()
	})
}
