// Package archive is the optional durable failed-job archive. Attach Store as a
// jobs.FailureSink (jobs.WithFailureSink or jobs.RegisterFailureSink) and every
// terminal failure a worker confirms is written to PostgreSQL with its complete
// envelope, independently of queue retention. Operators list archived
// failures, re-dispatch one as a new job and prune old entries.
package archive

import (
	"context"
	"fmt"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/migrate"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/jobarchive"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

const (
	MigrationOrigin migrate.Origin  = "foundry.jobs.archive"
	CreateArchive   migrate.ID      = "000001_create_failed_jobs"
	StoreEnvelopes  migrate.ID      = "000002_store_original_envelopes"
	Introduced      migrate.Version = "v0.1.0"
)

// Migrations are explicit ordinary migrations; use the same schema as New.
// 000002 stores each envelope's original bytes as text: jsonb re-renders the
// document (spacing, number expansion), so its text length neither matched the
// enforced envelope bound nor round-tripped for retry. Rows archived before it
// keep their jsonb rendering, which the previous CHECK already bounded.
func Migrations() []migrate.Definition {
	return []migrate.Definition{{Key: migrate.Key{Origin: MigrationOrigin, ID: CreateArchive}, Version: Introduced, SQL: []string{
		`CREATE TABLE foundry_failed_jobs (
id uuid PRIMARY KEY,
execution uuid NOT NULL,
queue text NOT NULL,
name text NOT NULL,
version bigint NOT NULL CHECK (version BETWEEN 1 AND 4294967295),
envelope jsonb NOT NULL CHECK (octet_length(envelope::text) <= 1310720),
reason text NOT NULL,
attempts bigint NOT NULL CHECK (attempts BETWEEN 0 AND 4294967295),
exceptions bigint NOT NULL CHECK (exceptions BETWEEN 0 AND 4294967295),
retries bigint NOT NULL CHECK (retries BETWEEN 0 AND 4294967295),
failed_at timestamptz NOT NULL CHECK (isfinite(failed_at)),
retried_at timestamptz CHECK (isfinite(retried_at)),
UNIQUE (execution, retries)
)`,
		`CREATE INDEX foundry_failed_jobs_failed ON foundry_failed_jobs (failed_at, id)`,
	}}, {Key: migrate.Key{Origin: MigrationOrigin, ID: StoreEnvelopes}, Version: Introduced, SQL: []string{
		`ALTER TABLE foundry_failed_jobs DROP CONSTRAINT IF EXISTS foundry_failed_jobs_envelope_check`,
		`ALTER TABLE foundry_failed_jobs ALTER COLUMN envelope TYPE text USING envelope::text`,
		`ALTER TABLE foundry_failed_jobs ADD CONSTRAINT foundry_failed_jobs_envelope_bytes CHECK (octet_length(envelope) <= 1310720)`,
	}}}
}

type Archived struct{}

// ID identifies one archived failure (not the job's execution ID).
type ID = model.ID[Archived]

// Store writes and reads the archive in one schema of a borrowed pool.
type Store struct {
	db     *database.DB
	schema string
	clock  clock.Clock
}

var _ jobs.FailureSink = (*Store)(nil)

func New(db *database.DB, schema string, source clock.Clock) (*Store, error) {
	if db == nil || !sqlname.Valid(schema) || source == nil {
		return nil, fault.New(fault.Invalid, "job archive requires a database, schema and clock")
	}
	return &Store{db: db, schema: schema, clock: source}, nil
}
func (*Store) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("job failure archive")) }

func (s *Store) within(ctx context.Context, fn func(*database.Tx) error, options ...database.TxOptions) error {
	return s.db.Transaction(ctx, func(tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL search_path TO "`+s.schema+`", pg_temp`); err != nil {
			return err
		}
		return fn(tx)
	}, options...)
}

// RecordFailure archives one confirmed failure. A repeated record of the same
// execution and manual retry cycle is ignored.
func (s *Store) RecordFailure(ctx context.Context, failed jobs.FailedJob) error {
	if s == nil || ctx == nil {
		return fault.New(fault.Invalid, "job archive is not initialized")
	}
	// MarshalJSON enforces jobs.MaxEnvelopeBytes on these exact bytes, which
	// are stored unchanged so Retry decodes what the queue carried.
	data, err := failed.Envelope.MarshalJSON()
	if err != nil {
		return err
	}
	if len(data) > jobs.MaxEnvelopeBytes {
		return fault.New(fault.Invalid, "job envelope exceeds transport bounds")
	}
	failedAt := failed.FailedAt
	if failedAt.IsZero() {
		failedAt = s.clock.Now()
	}
	at, err := temporal.NewDateTime(failedAt.UTC().Truncate(time.Microsecond))
	if err != nil {
		return err
	}
	id, err := model.NewID[jobarchive.Failure]()
	if err != nil {
		return err
	}
	draft := jobarchive.FailureDraft{}.SetID(id).SetExecution(failed.Envelope.ID()).SetQueue(string(failed.Queue)).SetName(string(failed.Envelope.Name())).
		SetVersion(uint32(failed.Envelope.Version())).SetEnvelope(string(data)).SetReason(string(failed.Reason)).SetAttempts(failed.Attempts).
		SetExceptions(failed.Exceptions).SetRetries(failed.Retries).SetFailedAt(at)
	fields := jobarchive.FailureFields()
	return s.within(ctx, func(tx *database.Tx) error {
		_, err := jobarchive.QueryFoundryFailedJobs().Upsert(ctx, tx, draft, query.OnConflict(fields.Execution, fields.Retries).DoNothing())
		return err
	})
}

// Entry is safe operator metadata for one archived failure: never the payload.
type Entry struct {
	ID         ID               `json:"id"`
	Execution  jobs.ExecutionID `json:"execution"`
	Queue      jobs.Queue       `json:"queue"`
	Name       jobs.Name        `json:"name"`
	Version    jobs.Version     `json:"version"`
	Reason     jobs.Reason      `json:"reason"`
	Attempts   uint32           `json:"attempts"`
	Exceptions uint32           `json:"exceptions"`
	Retries    uint32           `json:"retries"`
	FailedAt   time.Time        `json:"failed_at"`
	RetriedAt  time.Time        `json:"retried_at,omitzero"`
}

// MaxPage bounds one listing.
const MaxPage = 100

// ListOptions select a page of archived failures, newest first. After is the
// Next value of the previous page, with the same Name; Name narrows to one job
// name.
type ListOptions struct {
	Name  jobs.Name
	After Cursor
	Limit int
}

// Page is one bounded listing; Next continues it when nonzero.
type Page struct {
	Entries []Entry `json:"entries"`
	Next    Cursor  `json:"next,omitzero"`
}

// Cursor continues a listing after a page's last entry. It carries that
// entry's (failed_at, id) position, so a later page neither repeats nor skips
// entries when the last entry has since been pruned. It binds to the listing's
// Name filter and grants no authority.
type Cursor struct{ token string }

// ParseCursor restores a cursor printed by a previous listing.
func ParseCursor(text string) (Cursor, error) {
	if _, err := query.ParseCursor[jobarchive.Failure](text); err != nil {
		return Cursor{}, err
	}
	return Cursor{token: text}, nil
}
func (c Cursor) IsZero() bool                 { return c.token == "" }
func (c Cursor) String() string               { return c.token }
func (c Cursor) MarshalText() ([]byte, error) { return []byte(c.token), nil }
func (c *Cursor) UnmarshalText(text []byte) error {
	parsed, err := ParseCursor(string(text))
	*c = parsed
	return err
}

func (s *Store) List(ctx context.Context, options ListOptions) (Page, error) {
	if options.Limit == 0 {
		options.Limit = 20
	}
	if s == nil || ctx == nil || options.Limit < 1 || options.Limit > MaxPage {
		return Page{}, fault.New(fault.Invalid, "job archive listing requires 1 to 100 entries")
	}
	request := query.CursorRequest[jobarchive.Failure]{Size: options.Limit}
	if !options.After.IsZero() {
		cursor, err := query.ParseCursor[jobarchive.Failure](options.After.token)
		if err != nil {
			return Page{}, err
		}
		request.After = value.Set(cursor)
	}
	fields := jobarchive.FailureFields()
	var page Page
	err := s.within(ctx, func(tx *database.Tx) error {
		page = Page{}
		listing := jobarchive.QueryFoundryFailedJobs().OrderBy(fields.FailedAt.Desc(), fields.ID.Desc())
		if options.Name != "" {
			listing = listing.Where(fields.Name.Eq(string(options.Name)))
		}
		result, err := listing.CursorPaginate(ctx, tx, request)
		if err != nil {
			return err
		}
		for _, row := range result.Items {
			page.Entries = append(page.Entries, entry(row))
		}
		if next, ok := result.Next.Get(); ok {
			page.Next = Cursor{token: next.Token()}
		}
		return nil
	}, database.TxOptions{ReadOnly: true})
	return page, err
}

func entry(row jobarchive.Failure) Entry {
	result := Entry{ID: model.IDFromBytes[Archived](row.ID.Bytes()), Execution: row.Execution, Queue: jobs.Queue(row.Queue), Name: jobs.Name(row.Name), Version: jobs.Version(row.Version),
		Reason: jobs.Reason(row.Reason), Attempts: row.Attempts, Exceptions: row.Exceptions, Retries: row.Retries, FailedAt: row.FailedAt.UTC()}
	if retried, ok := row.RetriedAt.Get(); ok {
		result.RetriedAt = retried.UTC()
	}
	return result
}

// Retry re-dispatches one archived failure as a new job through dispatcher
// (see jobs.Dispatcher.Redispatch) and records when it was retried. Retrying the
// same entry again creates another job: check RetriedAt first. The new job's
// ID is returned.
func (s *Store) Retry(ctx context.Context, dispatcher *jobs.Dispatcher, id ID) (jobs.ExecutionID, error) {
	if s == nil || ctx == nil || dispatcher == nil || id.IsZero() {
		return jobs.ExecutionID{}, fault.New(fault.Invalid, "job archive retry requires a dispatcher and entry")
	}
	key := model.IDFromBytes[jobarchive.Failure](id.Bytes())
	var envelope jobs.Envelope
	err := s.within(ctx, func(tx *database.Tx) error {
		row, err := jobarchive.QueryFoundryFailedJobs().RequireFind(ctx, tx, key)
		if err != nil {
			return err
		}
		envelope, err = jobs.DecodeEnvelope([]byte(row.Envelope))
		return err
	}, database.TxOptions{ReadOnly: true})
	if err != nil {
		return jobs.ExecutionID{}, err
	}
	execution, err := dispatcher.Redispatch(ctx, envelope)
	if err != nil {
		return jobs.ExecutionID{}, err
	}
	now, err := temporal.NewDateTime(s.clock.Now().UTC().Truncate(time.Microsecond))
	if err != nil {
		return execution, err
	}
	err = s.within(ctx, func(tx *database.Tx) error {
		_, err := jobarchive.QueryFoundryFailedJobs().Update(ctx, tx, key, jobarchive.FailureDraft{}.SetRetriedAt(now))
		return err
	})
	return execution, err
}

// MaxPruneBatch bounds the entries one Prune transaction deletes.
const MaxPruneBatch = 10000

// Prune deletes up to limit entries archived before cutoff, oldest first.
// Repeat until fewer than limit are deleted.
func (s *Store) Prune(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	if limit == 0 {
		limit = 1000
	}
	if s == nil || ctx == nil || cutoff.IsZero() || limit < 1 || limit > MaxPruneBatch {
		return 0, fault.New(fault.Invalid, "job archive prune requires a cutoff and batch limit")
	}
	before, err := temporal.NewDateTime(cutoff.UTC().Truncate(time.Microsecond))
	if err != nil {
		return 0, err
	}
	fields := jobarchive.FailureFields()
	deleted := 0
	err = s.within(ctx, func(tx *database.Tx) error {
		// One bounded DELETE ... WHERE id IN (SELECT id ... LIMIT n): no row
		// (envelope included) is loaded to choose the batch.
		oldest := query.SelectValue(jobarchive.QueryFoundryFailedJobs().Where(fields.FailedAt.Lt(before)).OrderBy(fields.FailedAt.Asc(), fields.ID.Asc()).Limit(limit), fields.ID.Value())
		removed, err := jobarchive.QueryFoundryFailedJobs().Where(fields.ID.InQuery(oldest)).DeleteAll(ctx, tx)
		deleted = int(removed)
		return err
	})
	return deleted, err
}
