package schedule

import (
	"context"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/lease"
)

// ID is the semantic identifier of a registered schedule.
type ID string

// Handler handles one scheduled invocation and must cooperate with context cancellation.
type Handler func(context.Context, Invocation) error

// CatchUp is opt-in bounded replay on startup, leadership acquisition and missed
// ticks. Zero skips missed occurrences. At most Max occurrences from Window are
// considered per schedule per tick; excess backlog is skipped, never accumulated.
type CatchUp struct {
	Window time.Duration
	Max    int
}

// Validate checks catch-up window and count bounds. The zero policy disables catch-up.
func (c CatchUp) Validate() error {
	if c == (CatchUp{}) {
		return nil
	}
	if c.Window <= 0 || c.Window > 7*24*time.Hour || c.Max < 1 || c.Max > 1000 {
		return safeerror.NewFault(fault.Invalid, "invalid schedule catch-up window or count")
	}
	return nil
}

// Options configure one declaration. Days, Between and LastDayOfMonth narrow
// the spec in its zone without callbacks: a filtered occurrence advances the
// schedule silently. When runs as an owned callback before the Before hook; a
// false result records the occurrence as Skipped with reason Filtered (not
// logged as a failure), and an error fails it as a hook failure.
// EvenInMaintenanceMode keeps admitting this schedule while the application's
// maintenance gate is paused or draining.
type Options struct {
	Timeout               time.Duration
	WithoutOverlap        bool
	OverlapTTL            time.Duration
	Environments          []string
	CatchUp               CatchUp
	Days                  []time.Weekday
	Between               Window
	LastDayOfMonth        bool
	When                  Predicate
	EvenInMaintenanceMode bool
	Before                Handler
	After                 Handler
	Failed                func(context.Context, Invocation, error) error
}

// DefaultOptions returns the default execution timeout and overlap lease duration.
func DefaultOptions() Options { return Options{Timeout: 5 * time.Minute, OverlapTTL: 30 * time.Second} }

// Validate checks timeout, overlap, catch-up, calendar-filter, and environment bounds.
func (o Options) Validate() error {
	if o.Timeout <= 0 || o.Timeout > 24*time.Hour || len(o.Environments) > 64 {
		return safeerror.NewFault(fault.Invalid, "invalid schedule timeout or environment bounds")
	}
	if err := lease.ValidateDuration(o.OverlapTTL); err != nil {
		return err
	}
	if err := o.CatchUp.Validate(); err != nil {
		return err
	}
	if err := validateDays(o.Days); err != nil {
		return err
	}
	if err := o.Between.Validate(); err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, environment := range o.Environments {
		if !keyspace.ValidName(environment) || seen[environment] {
			return safeerror.NewFault(fault.Invalid, "invalid or duplicate schedule environment")
		}
		seen[environment] = true
	}
	return nil
}
func (o Options) snapshot() Options {
	o.Environments, o.Days = slices.Clone(o.Environments), slices.Clone(o.Days)
	return o
}

// Declaration freezes parsed timing and domain callbacks. No I/O or background
// activity occurs during declaration/registration. Services are constructor-injected.
type Declaration struct {
	id      ID
	spec    Spec
	handler Handler
	options Options
}

// Define validates a declaration and snapshots its mutable option slices without performing I/O.
func Define(id ID, spec Spec, handler Handler, options Options) (Declaration, error) {
	d := Declaration{id: id, spec: spec, handler: handler, options: options.snapshot()}
	return d, d.Validate()
}

// ID returns the schedule identifier.
func (d Declaration) ID() ID { return d.id }

// Spec returns the immutable parsed timing specification.
func (d Declaration) Spec() Spec { return d.spec }

// Options returns the execution policy with owned copies of mutable slices.
func (d Declaration) Options() Options { return d.options.snapshot() }

// Validate checks the identifier, handler, timing specification, and execution options.
func (d Declaration) Validate() error {
	if !identifier.Semantic(string(d.id)) || d.handler == nil {
		return safeerror.NewFault(fault.Invalid, "schedule requires a semantic ID and handler")
	}
	if err := d.spec.Validate(); err != nil {
		return err
	}
	return d.options.Validate()
}

// With returns a validated declaration with replacement options, leaving the receiver unchanged.
func (d Declaration) With(options Options) (Declaration, error) {
	return Define(d.id, d.spec, d.handler, options)
}

// Cron parses an expression in an explicit timezone and declares it with default options.
func Cron(id ID, expression string, zone *time.Location, handler Handler) (Declaration, error) {
	spec, err := ParseCron(expression, zone)
	if err != nil {
		return Declaration{}, err
	}
	return Define(id, spec, handler, DefaultOptions())
}

// Every declares an elapsed-time interval aligned to the UTC Unix epoch with default options.
func Every(id ID, every time.Duration, handler Handler) (Declaration, error) {
	spec, err := Interval(every)
	if err != nil {
		return Declaration{}, err
	}
	return Define(id, spec, handler, DefaultOptions())
}

// DailyAt declares a daily HH:MM occurrence in zone with default options.
func DailyAt(id ID, text string, zone *time.Location, handler Handler) (Declaration, error) {
	spec, err := dailySpec(text, zone)
	if err != nil {
		return Declaration{}, err
	}
	return Define(id, spec, handler, DefaultOptions())
}

// Hourly declares an occurrence at the start of each local hour in zone.
func Hourly(id ID, zone *time.Location, handler Handler) (Declaration, error) {
	return Cron(id, "0 0 * * * *", zone, handler)
}

// Daily declares an occurrence at local midnight in zone.
func Daily(id ID, zone *time.Location, handler Handler) (Declaration, error) {
	return DailyAt(id, "00:00", zone, handler)
}

// Weekly declares an occurrence at local midnight each Monday in zone.
func Weekly(id ID, zone *time.Location, handler Handler) (Declaration, error) {
	return Cron(id, "0 0 0 * * MON", zone, handler)
}

// Monthly declares an occurrence at local midnight on the first of each month in zone.
func Monthly(id ID, zone *time.Location, handler Handler) (Declaration, error) {
	return Cron(id, "0 0 0 1 * *", zone, handler)
}

// EveryMinute and the Every*Minutes helpers run on local wall-clock minute
// boundaries in zone (for example :00, :05, :10 for EveryFiveMinutes).
func EveryMinute(id ID, zone *time.Location, handler Handler) (Declaration, error) {
	return Cron(id, "0 * * * * *", zone, handler)
}

// EveryFiveMinutes declares occurrences at local minutes divisible by five in zone.
func EveryFiveMinutes(id ID, zone *time.Location, handler Handler) (Declaration, error) {
	return Cron(id, "0 */5 * * * *", zone, handler)
}

// EveryTenMinutes declares occurrences at local minutes divisible by ten in zone.
func EveryTenMinutes(id ID, zone *time.Location, handler Handler) (Declaration, error) {
	return Cron(id, "0 */10 * * * *", zone, handler)
}

// EveryFifteenMinutes declares occurrences at local minutes divisible by fifteen in zone.
func EveryFifteenMinutes(id ID, zone *time.Location, handler Handler) (Declaration, error) {
	return Cron(id, "0 */15 * * * *", zone, handler)
}

// EveryThirtyMinutes declares occurrences at local minutes zero and thirty in zone.
func EveryThirtyMinutes(id ID, zone *time.Location, handler Handler) (Declaration, error) {
	return Cron(id, "0 */30 * * * *", zone, handler)
}

// LastDayOfMonthAt runs at HH:MM on the last local day of each month.
func LastDayOfMonthAt(id ID, text string, zone *time.Location, handler Handler) (Declaration, error) {
	d, err := DailyAt(id, text, zone, handler)
	if err != nil {
		return Declaration{}, err
	}
	options := d.Options()
	options.LastDayOfMonth = true
	return d.With(options)
}

// Registry retains validated declarations with owned option slices.
type Registry struct{ entries []Declaration }

// NewRegistry validates and snapshots 1 to 4096 declarations, rejecting duplicate schedule IDs.
func NewRegistry(declarations ...Declaration) (*Registry, error) {
	if len(declarations) == 0 || len(declarations) > 4096 {
		return nil, safeerror.NewFault(fault.Invalid, "schedule registry requires 1 to 4096 declarations")
	}
	seen := make(map[ID]bool)
	entries := make([]Declaration, len(declarations))
	for i, d := range declarations {
		if err := d.Validate(); err != nil {
			return nil, err
		}
		if seen[d.id] {
			return nil, safeerror.NewFault(fault.Duplicate, "schedule ID already registered")
		}
		seen[d.id] = true
		d.options = d.options.snapshot()
		entries[i] = d
	}
	return &Registry{entries: entries}, nil
}
