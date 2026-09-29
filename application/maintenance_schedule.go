package application

import (
	"context"
	"hash/fnv"
	"log/slog"
	"math"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/audit"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/prune"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/idempotency"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/jobs/archive"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/notifications"
	"github.com/weiloon1234/Foundry-Go/outbox/publisher"
	"github.com/weiloon1234/Foundry-Go/schedule"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// MaintenanceTaskSettings tune one housekeeping task. Zero fields inherit
// MaintenanceScheduleSettings.Defaults.
type MaintenanceTaskSettings struct {
	// Disabled turns this task off.
	Disabled bool
	// Interval separates runs. Each task keeps a stable offset within its
	// interval, so housekeeping does not pile onto the top of every hour.
	Interval time.Duration
	// Batch bounds the rows one statement or transaction removes; MaxBatches
	// bounds the batches of one run, so a backlog continues at the next run.
	Batch      int
	MaxBatches int
	// Timeout bounds one run.
	Timeout time.Duration
	// Retention keeps younger rows of stores pruned by age (outbox, job archive
	// and notification inbox).
	Retention time.Duration
}

// MaintenanceScheduleSettings (features.maintenance) register leader-only
// housekeeping schedules in processes that run the scheduler kernel. Each
// enabled framework store with a prune API gets one schedule; application
// stores join through FeatureDeclarations.Pruning. Processes without the
// scheduler register nothing, and declarations are ignored while disabled.
type MaintenanceScheduleSettings struct {
	Enabled  bool
	Defaults MaintenanceTaskSettings
	// Outbox removes published outbox rows completed before Retention; pending
	// and failed rows are never pruned.
	Outbox MaintenanceTaskSettings
	// Idempotency removes expired outcomes, but only while the store's own
	// pruner is disabled (features.idempotency.config.prune_interval = 0).
	Idempotency MaintenanceTaskSettings
	// Audit applies features.audit.config.retention_days to the configured
	// area; zero retention registers no audit task. MaxBatches does not apply:
	// one run removes every expired batch within its Timeout.
	Audit MaintenanceTaskSettings
	// JobArchive removes archived failed jobs older than Retention.
	JobArchive MaintenanceTaskSettings
	// Notifications removes inbox records older than Retention. Unread records
	// are kept unless NotificationsUnread is set.
	Notifications       MaintenanceTaskSettings
	NotificationsUnread bool
	// Sessions, Tokens, Models and Custom tune declared Pruning of each kind.
	Sessions MaintenanceTaskSettings
	Tokens   MaintenanceTaskSettings
	Models   MaintenanceTaskSettings
	Custom   MaintenanceTaskSettings
}

func DefaultMaintenanceScheduleSettings() MaintenanceScheduleSettings {
	const day = 24 * time.Hour
	return MaintenanceScheduleSettings{
		Defaults:      MaintenanceTaskSettings{Interval: time.Hour, Batch: 500, MaxBatches: 20, Timeout: 5 * time.Minute},
		Outbox:        MaintenanceTaskSettings{Retention: 30 * day},
		JobArchive:    MaintenanceTaskSettings{Retention: 30 * day},
		Notifications: MaintenanceTaskSettings{Retention: 90 * day},
	}
}

// maxPruneBatches bounds the batches of one run; maxMaintenanceTasks bounds the
// schedules one application registers for housekeeping.
const (
	maxPruneBatches     = 10000
	maxMaintenanceTasks = 256
)

func (s MaintenanceScheduleSettings) resolve(t MaintenanceTaskSettings) MaintenanceTaskSettings {
	d := s.Defaults
	if t.Interval == 0 {
		t.Interval = d.Interval
	}
	if t.Batch == 0 {
		t.Batch = d.Batch
	}
	if t.MaxBatches == 0 {
		t.MaxBatches = d.MaxBatches
	}
	if t.Timeout == 0 {
		t.Timeout = d.Timeout
	}
	if t.Retention == 0 {
		t.Retention = d.Retention
	}
	return t
}

// validate checks one resolved task against its store's batch bound.
func (t MaintenanceTaskSettings) validate(limit int, byAge bool) error {
	if _, err := schedule.Interval(t.Interval); err != nil || t.Interval < time.Second {
		return fault.New(fault.Invalid, "maintenance interval must be whole milliseconds from one second to one year")
	}
	if t.Batch < 1 || t.Batch > limit || t.MaxBatches < 1 || t.MaxBatches > maxPruneBatches {
		return fault.New(fault.Invalid, "maintenance batch or batch count is outside its store's bound")
	}
	if t.Timeout <= 0 || t.Timeout > 24*time.Hour {
		return fault.New(fault.Invalid, "maintenance timeout must be positive and at most one day")
	}
	if byAge && t.Retention <= 0 {
		return fault.New(fault.Invalid, "maintenance retention must be positive")
	}
	return nil
}

type pruningKind uint8

const (
	sessionPruning pruningKind = iota + 1
	tokenPruning
	modelPruning
	customPruning
)

// pruneRun performs one bounded run. It reports removals committed before any
// failure and whether MaxBatches stopped it with work remaining.
type pruneRun func(context.Context, MaintenanceTaskSettings) (removed int64, remaining bool, err error)

// Pruning is one application-declared housekeeping task for the maintenance
// schedule. Construct it with PruneSessions, PruneTokens, PruneModels or
// PruneWith and return it in FeatureDeclarations.Pruning.
type Pruning struct {
	name  string
	kind  pruningKind
	limit int
	run   pruneRun
}

func (p Pruning) validate() error {
	if !identifier.Semantic(p.name) || p.run == nil || p.limit < 1 || p.kind < sessionPruning || p.kind > customPruning {
		return fault.New(fault.Invalid, "invalid maintenance pruning declaration")
	}
	return nil
}

// PruneSessions schedules removal of the guard's expired sessions
// (Sessions.Prune) as task sessions.<guard>.
func PruneSessions[M model.Identifiable, K any](guard BrowserGuard[M, K]) Pruning {
	sessions := guard.Sessions
	if sessions == nil {
		return Pruning{}
	}
	return Pruning{name: "sessions." + string(sessions.Guard().Name()), kind: sessionPruning, limit: session.MaxPageSize, run: batches(func(ctx context.Context, limit int) (int64, error) {
		removed, err := sessions.Prune(ctx, limit)
		return int64(removed), err
	})}
}

// PruneTokens schedules removal of the guard's expired token families
// (Tokens.Prune) as task tokens.<guard>.
func PruneTokens[M model.Identifiable, K any](guard TokenGuard[M, K]) Pruning {
	tokens := guard.Tokens
	if tokens == nil {
		return Pruning{}
	}
	return Pruning{name: "tokens." + string(tokens.Guard().Name()), kind: tokenPruning, limit: token.MaxPruneFamilies, run: batches(func(ctx context.Context, limit int) (int64, error) {
		removed, err := tokens.Prune(ctx, limit)
		return int64(removed), err
	})}
}

// PruneModels schedules prunable model declarations (database/prune) on one
// configured database connection, empty selecting the default, as task
// models.<connection>. declare runs at the start of every run with the
// application's current time, so selections can use cutoffs relative to it;
// it is also run once here to reject invalid declarations at Build. Declare
// every target of one connection in one call.
func PruneModels(s Services, connection database.ConnectionName, declare func(now time.Time) ([]prune.Target, error)) (Pruning, error) {
	if declare == nil || s.clock == nil {
		return Pruning{}, fault.New(fault.Invalid, "model pruning requires a declaration constructor and application services")
	}
	registry := func(now time.Time) (*prune.Registry, error) {
		targets, err := declare(now)
		if err != nil {
			return nil, err
		}
		if len(targets) == 0 {
			return nil, fault.New(fault.Invalid, "model pruning requires prunable declarations")
		}
		return prune.New(targets...)
	}
	if _, err := registry(s.clock.Now()); err != nil {
		return Pruning{}, err
	}
	if connection == "" {
		connection = s.Databases.DefaultName()
	}
	db, err := s.Databases.Connection(connection)
	if err != nil {
		return Pruning{}, err
	}
	source := s.clock
	return Pruning{name: "models." + string(connection), kind: modelPruning, limit: query.MaxPerModelWriteRows, run: func(ctx context.Context, t MaintenanceTaskSettings) (int64, bool, error) {
		models, err := registry(source.Now())
		if err != nil {
			return 0, false, err
		}
		result, err := models.Run(ctx, db, prune.Options{BatchSize: t.Batch, MaxBatches: t.MaxBatches})
		var removed int64
		remaining := false
		for _, count := range result.Counts {
			removed += count.Removed
			remaining = remaining || count.Remaining
		}
		return removed, remaining, err
	}}, nil
}

// PruneWith schedules another store's bounded prune operation, such as an
// auth challenge flow, email verification, password reset or MFA factor store,
// as task name. step removes at most limit rows and reports how many; limit
// never exceeds maxBatch, the store's own bound, which the Custom batch must fit.
func PruneWith(name string, maxBatch int, step func(ctx context.Context, limit int) (int64, error)) Pruning {
	if step == nil {
		return Pruning{}
	}
	return Pruning{name: name, kind: customPruning, limit: maxBatch, run: batches(step)}
}

// batches repeats step until a short batch, MaxBatches or cancellation.
func batches(step func(context.Context, int) (int64, error)) pruneRun {
	return func(ctx context.Context, t MaintenanceTaskSettings) (int64, bool, error) {
		var total int64
		for range t.MaxBatches {
			if err := ctx.Err(); err != nil {
				return total, false, err
			}
			removed, err := step(ctx, t.Batch)
			if removed > 0 {
				total += removed
			}
			if err != nil {
				return total, false, err
			}
			if removed < int64(t.Batch) {
				return total, false, nil
			}
		}
		return total, true, nil
	}
}

// aged derives one cutoff per run from the application clock and Retention.
func aged(source clock.Clock, step func(ctx context.Context, cutoff time.Time, limit int) (int, error)) pruneRun {
	return func(ctx context.Context, t MaintenanceTaskSettings) (int64, bool, error) {
		cutoff := source.Now().UTC().Add(-t.Retention)
		return batches(func(ctx context.Context, limit int) (int64, error) {
			removed, err := step(ctx, cutoff, limit)
			return int64(removed), err
		})(ctx, t)
	}
}

// maintenanceTask is one resolved schedule entry before its store is bound.
type maintenanceTask struct {
	name     string
	settings MaintenanceTaskSettings
	bind     func(Services) (pruneRun, error)
}

// frameworkMaintenanceTasks selects the enabled framework stores. It runs at
// Build, so invalid settings fail before any resource is acquired.
func frameworkMaintenanceTasks(s Settings, source clock.Clock) ([]maintenanceTask, error) {
	m, f := s.Features.Maintenance, s.Features
	var tasks []maintenanceTask
	add := func(enabled bool, name string, overrides MaintenanceTaskSettings, limit int, byAge bool, bind func(Services) (pruneRun, error)) error {
		if !enabled || overrides.Disabled {
			return nil
		}
		task := maintenanceTask{name: name, settings: m.resolve(overrides), bind: bind}
		if err := task.settings.validate(limit, byAge); err != nil {
			return err
		}
		tasks = append(tasks, task)
		return nil
	}
	errs := []error{
		add(f.Outbox.Enabled, "outbox", m.Outbox, publisher.MaxOperationBatch, true, func(s Services) (pruneRun, error) {
			p, err := s.OutboxPublisher()
			if err != nil {
				return nil, err
			}
			return aged(source, p.Prune), nil
		}),
		// A store-owned pruner already runs on every started store; never both.
		add(f.Idempotency.Enabled && f.Idempotency.Config.PruneInterval == 0, "idempotency", m.Idempotency, idempotency.MaxPrune, false, func(s Services) (pruneRun, error) {
			store, err := s.Idempotency()
			if err != nil {
				return nil, err
			}
			return batches(store.Prune), nil
		}),
		add(f.Audit.Enabled && f.Audit.Config.RetentionDays > 0, "audit", m.Audit, query.MaxPageSize, false, func(s Services) (pruneRun, error) {
			scope, err := s.AuditScope()
			if err != nil {
				return nil, err
			}
			return auditRun(source, scope), nil
		}),
		add(s.Worker.Archive.Enabled, "jobs.archive", m.JobArchive, archive.MaxPruneBatch, true, func(s Services) (pruneRun, error) {
			store, err := s.JobArchive()
			if err != nil {
				return nil, err
			}
			return aged(source, store.Prune), nil
		}),
		add(f.Notifications.Enabled, "notifications.inbox", m.Notifications, notifications.MaxPruneBatch, true, func(s Services) (pruneRun, error) {
			manager, err := s.Notifications()
			if err != nil {
				return nil, err
			}
			readOnly := !m.NotificationsUnread
			return aged(source, func(ctx context.Context, cutoff time.Time, limit int) (int, error) {
				return manager.PruneInbox(ctx, cutoff, readOnly, limit)
			}), nil
		}),
	}
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return tasks, nil
}

// auditRun prunes the configured area with its own retention in bounded
// transactions until a short batch or the run's deadline.
func auditRun(source clock.Clock, scope *audit.Scope) pruneRun {
	return func(ctx context.Context, t MaintenanceTaskSettings) (int64, bool, error) {
		now, err := temporal.Now(source)
		if err != nil {
			return 0, false, err
		}
		removed, err := scope.PruneRetention(ctx, now, t.Batch)
		return removed, false, err
	}
}

// declared returns the overrides of one declared pruning kind.
func (s MaintenanceScheduleSettings) declared(kind pruningKind) MaintenanceTaskSettings {
	switch kind {
	case sessionPruning:
		return s.Sessions
	case tokenPruning:
		return s.Tokens
	case modelPruning:
		return s.Models
	default:
		return s.Custom
	}
}

// maintenanceSchedules validates features.maintenance and returns the
// constructor of its leader-only schedules, or nil when the feature or this
// process's scheduler is disabled.
func maintenanceSchedules(s Settings, source clock.Clock) (Schedules, error) {
	m := s.Features.Maintenance
	if !m.Enabled {
		return nil, nil
	}
	// Declared kinds are checked against their store bounds before any
	// declaration exists; a custom store's bound is checked per declaration.
	for _, kind := range []struct {
		kind  pruningKind
		limit int
	}{{sessionPruning, session.MaxPageSize}, {tokenPruning, token.MaxPruneFamilies}, {modelPruning, query.MaxPerModelWriteRows}, {customPruning, math.MaxInt32}} {
		if err := m.resolve(m.declared(kind.kind)).validate(kind.limit, false); err != nil {
			return nil, err
		}
	}
	tasks, err := frameworkMaintenanceTasks(s, source)
	if err != nil || !s.Scheduler.Enabled {
		return nil, err
	}
	return func(services Services) ([]schedule.Declaration, error) {
		declared, err := Resolve(services, featureDeclarationsKey)
		if err != nil {
			return nil, err
		}
		bound := slices.Clone(tasks)
		runs := make([]pruneRun, 0, len(bound)+len(declared.Pruning))
		for _, task := range bound {
			run, err := task.bind(services)
			if err != nil {
				return nil, err
			}
			runs = append(runs, run)
		}
		for _, p := range declared.Pruning {
			if err := p.validate(); err != nil {
				return nil, err
			}
			overrides := m.declared(p.kind)
			if overrides.Disabled {
				continue
			}
			task := maintenanceTask{name: p.name, settings: m.resolve(overrides)}
			if err := task.settings.validate(p.limit, false); err != nil {
				return nil, err
			}
			bound, runs = append(bound, task), append(runs, p.run)
		}
		if len(bound) > maxMaintenanceTasks {
			return nil, fault.New(fault.Invalid, "too many maintenance tasks")
		}
		result := make([]schedule.Declaration, 0, len(bound))
		for i, task := range bound {
			d, err := maintenanceDeclaration(task, runs[i], services.Logger)
			if err != nil {
				return nil, err
			}
			result = append(result, d)
		}
		return result, nil
	}, nil
}

// maintenanceDeclaration builds one leader-only, non-overlapping schedule that
// logs committed removals. The scheduler records and logs failures.
func maintenanceDeclaration(task maintenanceTask, run pruneRun, logger *slog.Logger) (schedule.Declaration, error) {
	// A name-derived whole-second offset is the same on every instance, so a
	// new leader keeps the same occurrence identities.
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(task.name))
	offset := time.Duration(hash.Sum64()%uint64(task.settings.Interval/time.Second)) * time.Second
	spec, err := schedule.IntervalFrom(task.settings.Interval, time.Unix(0, 0).Add(offset))
	if err != nil {
		return schedule.Declaration{}, err
	}
	options := schedule.DefaultOptions()
	options.Timeout, options.WithoutOverlap = task.settings.Timeout, true
	settings := task.settings
	return schedule.Define(schedule.ID("foundry.maintenance."+task.name), spec, func(ctx context.Context, _ schedule.Invocation) error {
		removed, remaining, err := run(ctx, settings)
		if (removed > 0 || remaining) && logger != nil {
			logger.LogAttrs(ctx, slog.LevelInfo, "maintenance task pruned records", slog.String("task", task.name), slog.Int64("removed", removed), slog.Bool("remaining", remaining))
		}
		return err
	}, options)
}
