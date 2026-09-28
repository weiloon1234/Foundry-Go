package migrate

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type StepState string

const (
	StepReady     StepState = "ready"
	StepInFlight  StepState = "in_flight"
	StepUncertain StepState = "uncertain"
)

// Progress is a durable nontransactional checkpoint. Confirmed counts statements
// with a successfully recorded result. InFlight/Uncertain never authorize retry.
// Revision protects reconciliation against a stale inspection snapshot.
type Progress struct {
	Key       Key       `json:"key"`
	Version   Version   `json:"version"`
	Checksum  Checksum  `json:"checksum"`
	Batch     int64     `json:"batch"`
	Confirmed int       `json:"confirmed_statements"`
	State     StepState `json:"state"`
	Revision  int64     `json:"revision"`
	UpdatedAt time.Time `json:"updated_at"`
}

// StatementOutcome is an operator's explicit attestation after inspecting actual
// database state. Foundry does not infer whether an interrupted statement ran.
type StatementOutcome string

const (
	StatementApplied    StatementOutcome = "applied"
	StatementNotApplied StatementOutcome = "not_applied"
)

var ErrReconciliationRequired = errors.New("nontransactional migration requires reconciliation")

type InterruptedMigration struct {
	Progress Progress
	cause    error
}

func (*InterruptedMigration) Error() string {
	return "nontransactional migration interrupted; inspect progress and database state before resuming"
}
func (e *InterruptedMigration) Unwrap() error      { return e.cause }
func (*InterruptedMigration) Is(target error) bool { return target == ErrReconciliationRequired }

func (p *Postgres) ensureProgress(ctx context.Context, session *database.Session) error {
	needed := false
	for _, item := range p.registry.ordered {
		needed = needed || item.entry.Mode == NonTransactional
	}
	if !needed {
		return nil
	}
	_, err := session.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+p.progressTable+` (
origin text NOT NULL, id text NOT NULL, version text NOT NULL,
checksum text NOT NULL CHECK (checksum ~ '^[0-9a-f]{64}$'),
batch bigint NOT NULL CHECK (batch > 0),
confirmed integer NOT NULL CHECK (confirmed >= 0),
state text NOT NULL CHECK (state IN ('ready','in_flight','uncertain')),
revision bigint NOT NULL CHECK (revision > 0),
updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
PRIMARY KEY(origin,id))`)
	return err
}
func (p *Postgres) progress(ctx context.Context, executor database.Executor) ([]Progress, error) {
	var kind string
	err := database.ScanOne(ctx, executor, `SELECT c.relkind::text FROM pg_catalog.pg_class AS c JOIN pg_catalog.pg_namespace AS n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relname=$2`, []any{p.config.Schema, p.progressName}, &kind)
	if errors.Is(err, database.NotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if kind != "r" {
		return nil, fault.New(fault.Invalid, "migration progress relation must be an ordinary table")
	}
	var records []Progress
	err = database.ForEach(ctx, executor, "SELECT origin,id,version,checksum,batch,confirmed,state,revision,updated_at FROM "+p.progressTable+" ORDER BY origin,id LIMIT $1", []any{p.config.MaxHistory + 1}, func(row database.Row) (Progress, error) {
		var item Progress
		var checksum string
		if err := row.Scan(&item.Key.Origin, &item.Key.ID, &item.Version, &checksum, &item.Batch, &item.Confirmed, &item.State, &item.Revision, &item.UpdatedAt); err != nil {
			return item, err
		}
		parsed, err := ParseChecksum(checksum)
		if err != nil {
			return item, err
		}
		item.Checksum = parsed
		item.UpdatedAt = item.UpdatedAt.UTC()
		if !validKey(item.Key) || !validName(string(item.Version)) || item.Batch < 1 || item.Confirmed < 0 || item.Revision < 1 || item.UpdatedAt.IsZero() || item.State != StepReady && item.State != StepInFlight && item.State != StepUncertain {
			return item, fault.New(fault.Invalid, "invalid migration progress record")
		}
		return item, nil
	}, func(item Progress) error {
		if len(records) >= p.config.MaxHistory {
			return fault.New(fault.Invalid, "migration progress exceeds configured limit")
		}
		records = append(records, item)
		return nil
	})
	return records, err
}
func (p *Postgres) inspectProgress(ctx context.Context, executor database.Executor, history []Applied) (Report, error) {
	report, err := p.registry.Inspect(history)
	if err != nil {
		return Report{}, err
	}
	records, err := p.progress(ctx, executor)
	if err != nil {
		return Report{}, err
	}
	for _, record := range records {
		index := -1
		for i := range report.Statuses {
			if report.Statuses[i].Key == record.Key {
				index = i
				break
			}
		}
		if index < 0 {
			report.Statuses = append(report.Statuses, Status{Key: record.Key, State: Missing, Progress: &record})
			report.Problems = append(report.Problems, Problem{Code: DefinitionMissing, Key: record.Key})
			continue
		}
		status := &report.Statuses[index]
		status.Progress = &record
		item, exists := p.registry.byKey[record.Key]
		if !exists || status.Applied != nil || !matchesProgress(record, p.registry.ordered[item]) {
			status.State = Changed
			report.Problems = append(report.Problems, Problem{Code: ProgressMismatch, Key: record.Key})
			continue
		}
		status.State = Incomplete
		report.LastBatch = max(report.LastBatch, record.Batch)
		if record.State != StepReady {
			report.Problems = append(report.Problems, Problem{Code: ReconciliationRequired, Key: record.Key})
		}
	}
	p.inspectProgressDependencies(&report)
	return report, nil
}

// An unfinished migration proves its prerequisites were completed before its
// first statement. Missing prerequisite history is drift, not permission to
// rerun an earlier migration or reconcile against an inconsistent history.
func (p *Postgres) inspectProgressDependencies(report *Report) {
	for _, status := range report.Statuses {
		if status.Progress == nil || status.Definition == nil || status.Applied != nil {
			continue
		}
		for _, dependency := range status.Definition.Requires {
			index, exists := p.registry.byKey[dependency]
			if !exists || report.Statuses[index].Applied == nil {
				report.Problems = append(report.Problems, Problem{Code: DependencyNotApplied, Key: status.Key, Dependency: &dependency})
			}
		}
	}
}
func matchesProgress(record Progress, item migration) bool {
	return item.entry.Mode == NonTransactional && record.Version == item.entry.Version && record.Checksum == item.entry.Checksum && record.Confirmed <= len(item.statements) && (record.State == StepReady || record.Confirmed < len(item.statements))
}
func (p *Postgres) moveProgress(ctx context.Context, session *database.Session, current Progress, state StepState, confirmed int) (Progress, error) {
	if current.Revision == math.MaxInt64 {
		return current, fault.New(fault.Invalid, "migration progress revision is exhausted")
	}
	updated := current
	updated.State = state
	updated.Confirmed = confirmed
	updated.Revision++
	err := database.ScanOne(ctx, session, "UPDATE "+p.progressTable+" SET state=$1,confirmed=$2,revision=revision+1,updated_at=CURRENT_TIMESTAMP WHERE origin=$3 AND id=$4 AND version=$5 AND checksum=$6 AND revision=$7 AND state=$8 RETURNING updated_at", []any{string(state), confirmed, string(current.Key.Origin), string(current.Key.ID), string(current.Version), current.Checksum.String(), current.Revision, string(current.State)}, &updated.UpdatedAt)
	if err != nil {
		return current, err
	}
	updated.UpdatedAt = updated.UpdatedAt.UTC()
	return updated, nil
}
func (p *Postgres) applyNonTransactional(ctx context.Context, session *database.Session, item migration, batch int64) (Applied, error) {
	records, err := p.progress(ctx, session)
	if err != nil {
		return Applied{}, err
	}
	current := Progress{Key: item.entry.Key, Version: item.entry.Version, Checksum: item.entry.Checksum, Batch: batch, State: StepReady, Revision: 1}
	exists := false
	for _, record := range records {
		if record.Key == current.Key {
			current = record
			exists = true
			break
		}
	}
	if !exists {
		err = database.ScanOne(ctx, session, "INSERT INTO "+p.progressTable+" (origin,id,version,checksum,batch,confirmed,state,revision) VALUES($1,$2,$3,$4,$5,0,'ready',1) RETURNING updated_at", []any{string(current.Key.Origin), string(current.Key.ID), string(current.Version), current.Checksum.String(), batch}, &current.UpdatedAt)
		if err != nil {
			return Applied{}, err
		}
	}
	if !matchesProgress(current, item) || current.State != StepReady {
		return Applied{}, &InterruptedMigration{Progress: current, cause: ErrReconciliationRequired}
	}
	for current.Confirmed < len(item.statements) {
		running, err := p.moveProgress(ctx, session, current, StepInFlight, current.Confirmed)
		if err != nil {
			return Applied{}, &InterruptedMigration{Progress: current, cause: err}
		}
		current = running
		if _, err = session.Exec(ctx, item.statements[current.Confirmed]); err != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.config.CleanupTimeout)
			failed, markErr := p.moveProgress(cleanup, session, current, StepUncertain, current.Confirmed)
			cancel()
			if markErr == nil {
				current = failed
			}
			return Applied{}, &InterruptedMigration{Progress: current, cause: errors.Join(err, markErr)}
		}
		confirmed, err := p.moveProgress(ctx, session, current, StepReady, current.Confirmed+1)
		if err != nil {
			return Applied{}, &InterruptedMigration{Progress: current, cause: err}
		}
		current = confirmed
	}
	applied := Applied{Key: current.Key, Version: current.Version, Checksum: current.Checksum, Batch: current.Batch}
	// Only final bookkeeping is transactional; preceding SQL is already durable.
	err = session.Transaction(ctx, func(tx *database.Tx) error {
		if err := database.ScanOne(ctx, tx, "INSERT INTO "+p.table+" (origin,id,version,checksum,batch) VALUES($1,$2,$3,$4,$5) RETURNING applied_at", []any{string(applied.Key.Origin), string(applied.Key.ID), string(applied.Version), applied.Checksum.String(), applied.Batch}, &applied.AppliedAt); err != nil {
			return err
		}
		result, err := tx.Exec(ctx, "DELETE FROM "+p.progressTable+" WHERE origin=$1 AND id=$2 AND revision=$3", string(current.Key.Origin), string(current.Key.ID), current.Revision)
		if err != nil {
			return err
		}
		if result.RowsAffected != 1 {
			return fault.New(fault.Conflict, "migration progress changed during finalization")
		}
		return nil
	})
	if err != nil {
		return Applied{}, &InterruptedMigration{Progress: current, cause: err}
	}
	applied.AppliedAt = applied.AppliedAt.UTC()
	return applied, nil
}

// Reconcile records an operator-verified outcome for exactly the inspected
// uncertain statement under the runner's advisory lock. It executes no migration
// SQL. Inspect the catalog/data first; a wrong attestation can lose/repeat effects.
// Up then resumes from the resulting checkpoint. Stale snapshots fail closed.
func (p *Postgres) Reconcile(ctx context.Context, expected Progress, outcome StatementOutcome) (Progress, error) {
	if outcome != StatementApplied && outcome != StatementNotApplied {
		return Progress{}, fault.New(fault.Invalid, "invalid migration reconciliation outcome")
	}
	var result Progress
	err := p.locked(ctx, func(session *database.Session) error {
		records, err := p.progress(ctx, session)
		if err != nil {
			return err
		}
		for _, current := range records {
			if current.Key != expected.Key {
				continue
			}
			if current != expected || current.State == StepReady {
				return fault.New(fault.Conflict, "migration reconciliation snapshot is stale")
			}
			index, exists := p.registry.byKey[current.Key]
			if !exists || !matchesProgress(current, p.registry.ordered[index]) {
				return fault.New(fault.Conflict, "migration definition does not match interrupted progress")
			}
			history, err := p.history(ctx, session)
			if err != nil {
				return err
			}
			report, err := p.registry.Inspect(history)
			if err != nil {
				return err
			}
			report.Statuses[index].Progress = &current
			p.inspectProgressDependencies(&report)
			if err := report.Check(); err != nil {
				return err
			}
			for _, entry := range history {
				if entry.Key == current.Key {
					return fault.New(fault.Conflict, "migration already has completion history")
				}
			}
			confirmed := current.Confirmed
			if outcome == StatementApplied {
				confirmed++
			}
			result, err = p.moveProgress(ctx, session, current, StepReady, confirmed)
			return err
		}
		return fault.New(fault.Missing, "migration progress no longer exists")
	})
	return result, err
}
