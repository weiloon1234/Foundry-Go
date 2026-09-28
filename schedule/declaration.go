package schedule

import (
	"context"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
)

type ID string
type Handler func(context.Context, Invocation) error

// CatchUp is opt-in bounded replay on startup, leadership acquisition and missed
// ticks. Zero skips missed occurrences. At most Max occurrences from Window are
// considered per schedule per tick; excess backlog is skipped, never accumulated.
type CatchUp struct {
	Window time.Duration
	Max    int
}

func (c CatchUp) Validate() error {
	if c == (CatchUp{}) {
		return nil
	}
	if c.Window <= 0 || c.Window > 7*24*time.Hour || c.Max < 1 || c.Max > 1000 {
		return fault.New(fault.Invalid, "invalid schedule catch-up window or count")
	}
	return nil
}

type Options struct {
	Timeout        time.Duration
	WithoutOverlap bool
	OverlapTTL     time.Duration
	Environments   []string
	CatchUp        CatchUp
	Before         Handler
	After          Handler
	Failed         func(context.Context, Invocation, error) error
}

func DefaultOptions() Options { return Options{Timeout: 5 * time.Minute, OverlapTTL: 30 * time.Second} }
func (o Options) Validate() error {
	if o.Timeout <= 0 || o.Timeout > 24*time.Hour || len(o.Environments) > 64 {
		return fault.New(fault.Invalid, "invalid schedule timeout or environment bounds")
	}
	if err := lease.ValidateDuration(o.OverlapTTL); err != nil {
		return err
	}
	if err := o.CatchUp.Validate(); err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, environment := range o.Environments {
		if !keyspace.ValidName(environment) || seen[environment] {
			return fault.New(fault.Invalid, "invalid or duplicate schedule environment")
		}
		seen[environment] = true
	}
	return nil
}
func (o Options) snapshot() Options { o.Environments = slices.Clone(o.Environments); return o }

// Declaration freezes parsed timing and domain callbacks. No I/O or background
// activity occurs during declaration/registration. Services are constructor-injected.
type Declaration struct {
	id      ID
	spec    Spec
	handler Handler
	options Options
}

func Define(id ID, spec Spec, handler Handler, options Options) (Declaration, error) {
	d := Declaration{id: id, spec: spec, handler: handler, options: options.snapshot()}
	return d, d.Validate()
}
func (d Declaration) ID() ID           { return d.id }
func (d Declaration) Spec() Spec       { return d.spec }
func (d Declaration) Options() Options { return d.options.snapshot() }
func (d Declaration) Validate() error {
	if !identifier.Semantic(string(d.id)) || d.handler == nil {
		return fault.New(fault.Invalid, "schedule requires a semantic ID and handler")
	}
	if err := d.spec.Validate(); err != nil {
		return err
	}
	return d.options.Validate()
}
func (d Declaration) With(options Options) (Declaration, error) {
	return Define(d.id, d.spec, d.handler, options)
}

func Cron(id ID, expression string, zone *time.Location, handler Handler) (Declaration, error) {
	spec, err := ParseCron(expression, zone)
	if err != nil {
		return Declaration{}, err
	}
	return Define(id, spec, handler, DefaultOptions())
}
func Every(id ID, every time.Duration, handler Handler) (Declaration, error) {
	spec, err := Interval(every)
	if err != nil {
		return Declaration{}, err
	}
	return Define(id, spec, handler, DefaultOptions())
}
func DailyAt(id ID, text string, zone *time.Location, handler Handler) (Declaration, error) {
	spec, err := dailySpec(text, zone)
	if err != nil {
		return Declaration{}, err
	}
	return Define(id, spec, handler, DefaultOptions())
}
func Hourly(id ID, zone *time.Location, handler Handler) (Declaration, error) {
	return Cron(id, "0 0 * * * *", zone, handler)
}
func Daily(id ID, zone *time.Location, handler Handler) (Declaration, error) {
	return DailyAt(id, "00:00", zone, handler)
}
func Weekly(id ID, zone *time.Location, handler Handler) (Declaration, error) {
	return Cron(id, "0 0 0 * * MON", zone, handler)
}
func Monthly(id ID, zone *time.Location, handler Handler) (Declaration, error) {
	return Cron(id, "0 0 0 1 * *", zone, handler)
}

type Registry struct{ entries []Declaration }

func NewRegistry(declarations ...Declaration) (*Registry, error) {
	if len(declarations) == 0 || len(declarations) > 4096 {
		return nil, fault.New(fault.Invalid, "schedule registry requires 1 to 4096 declarations")
	}
	seen := make(map[ID]bool)
	entries := make([]Declaration, len(declarations))
	for i, d := range declarations {
		if err := d.Validate(); err != nil {
			return nil, err
		}
		if seen[d.id] {
			return nil, fault.New(fault.Duplicate, "schedule ID already registered")
		}
		seen[d.id] = true
		d.options = d.options.snapshot()
		entries[i] = d
	}
	return &Registry{entries: entries}, nil
}
