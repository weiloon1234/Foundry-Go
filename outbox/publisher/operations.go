package publisher

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/outboxstore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/outbox"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// MaxOperationBatch bounds the rows one operator transaction changes. Callers
// repeat a batch until it reports zero rows, so locks stay short.
const MaxOperationBatch = 10000

// Selection addresses failed rows for requeue or inspection. A zero ID and empty
// Kind select every failed row; ID selects exactly one; Kind selects one
// producer family such as "job" or "event".
type Selection struct {
	ID    MessageID
	Kind  string
	Limit int
}

func (s Selection) validate() (Selection, error) {
	if s.Limit == 0 {
		s.Limit = 1000
	}
	if s.Limit < 1 || s.Limit > MaxOperationBatch || s.Kind != "" && !identifier.Semantic(s.Kind) {
		return s, fault.New(fault.Invalid, "invalid outbox selection kind or batch limit")
	}
	return s, nil
}
func (s Selection) predicates() []query.Predicate[outboxstore.Message] {
	fields := outboxstore.MessageFields()
	result := []query.Predicate[outboxstore.Message]{fields.PublishState.Eq(outbox.PublicationFailed)}
	if !s.ID.IsZero() {
		result = append(result, fields.ID.Eq(model.IDFromBytes[outboxstore.Message](s.ID.Bytes())))
	}
	if s.Kind != "" {
		result = append(result, fields.Kind.Eq(s.Kind))
	}
	return result
}

// Requeue makes up to Limit failed rows pending again with a fresh attempt
// budget and immediate eligibility. Delivery remains at least once: the stable
// destination identity still deduplicates a row that was accepted before it
// failed. It returns the number of rows changed in this committed batch.
func (p *Publisher) Requeue(ctx context.Context, selection Selection) (int, error) {
	selection, err := selection.validate()
	if err != nil {
		return 0, err
	}
	if p == nil || p.writer == nil || ctx == nil {
		return 0, fault.New(fault.Invalid, "outbox requeue requires an initialized publisher and context")
	}
	now, err := temporal.NewDateTime(p.config.Clock.Now().Truncate(time.Microsecond))
	if err != nil {
		return 0, err
	}
	fields := outboxstore.MessageFields()
	changed := 0
	err = p.writer.Transaction(ctx, func(tx *database.Tx) error {
		changed = 0
		rows, err := outboxstore.QueryFoundryOutbox().Where(selection.predicates()...).OrderBy(fields.CreatedAt.Asc(), fields.ID.Asc()).ForUpdate().SkipLocked().Limit(selection.Limit).All(ctx, tx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			draft := outboxstore.MessageDraft{}.SetPublishState(outbox.Pending).SetPublishAttempts(0).SetPublishAfter(now).SetPublishReason("")
			if _, err := outboxstore.QueryFoundryOutbox().Update(ctx, tx, row.ID, draft); err != nil {
				return err
			}
			changed++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return changed, nil
}

// Prune deletes up to limit published rows whose publication completed before
// cutoff. Pending and failed rows are never pruned. Keep the retention longer
// than any window in which a consumer may reload a message by its outbox ID.
func (p *Publisher) Prune(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	if limit == 0 {
		limit = 1000
	}
	if p == nil || p.writer == nil || ctx == nil || limit < 1 || limit > MaxOperationBatch || cutoff.IsZero() {
		return 0, fault.New(fault.Invalid, "outbox prune requires a publisher, cutoff and batch limit")
	}
	before, err := temporal.NewDateTime(cutoff.UTC().Truncate(time.Microsecond))
	if err != nil {
		return 0, err
	}
	fields := outboxstore.MessageFields()
	deleted := 0
	err = p.writer.Transaction(ctx, func(tx *database.Tx) error {
		// One bounded DELETE ... WHERE id IN (SELECT id ... LIMIT n): no row
		// (payload included) is loaded to choose the batch. A concurrent prune
		// of the same rows waits, then deletes none of them twice.
		oldest := query.SelectValue(outboxstore.QueryFoundryOutbox().Where(fields.PublishState.Eq(outbox.Published), fields.PublishedAt.Lt(before)).OrderBy(fields.PublishedAt.Asc(), fields.ID.Asc()).Limit(limit), fields.ID.Value())
		removed, err := outboxstore.QueryFoundryOutbox().Where(fields.ID.InQuery(oldest), fields.PublishState.Eq(outbox.Published)).DeleteAll(ctx, tx)
		deleted = int(removed)
		return err
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

// Failure is safe operator metadata for one failed row: no payload, origin or
// error text. Reason is the bounded persisted classification.
type Failure struct {
	ID          MessageID          `json:"id"`
	Kind        string             `json:"kind"`
	Destination outbox.Destination `json:"destination"`
	Name        string             `json:"name"`
	Version     uint32             `json:"version"`
	Attempts    uint32             `json:"attempts"`
	Reason      string             `json:"reason"`
	CreatedAt   time.Time          `json:"created_at"`
}

// Failed lists up to Limit (at most 100) of the oldest failed rows matching the
// selection. Requeue or narrow by kind/ID to see further failures; Stats reports
// the total count.
func (p *Publisher) Failed(ctx context.Context, selection Selection) ([]Failure, error) {
	selection, err := selection.validate()
	if err != nil {
		return nil, err
	}
	if p == nil || p.writer == nil || ctx == nil || selection.Limit > 100 {
		return nil, fault.New(fault.Invalid, "outbox failure listing requires a publisher, context and at most 100 rows")
	}
	fields := outboxstore.MessageFields()
	var result []Failure
	err = p.writer.Transaction(ctx, func(tx *database.Tx) error {
		result = nil
		rows, err := outboxstore.QueryFoundryOutbox().Where(selection.predicates()...).OrderBy(fields.CreatedAt.Asc(), fields.ID.Asc()).Limit(selection.Limit).All(ctx, tx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			result = append(result, Failure{ID: model.IDFromBytes[Publication](row.ID.Bytes()), Kind: row.Kind, Destination: row.Destination, Name: row.Name, Version: row.Version, Attempts: row.PublishAttempts, Reason: row.PublishReason, CreatedAt: row.CreatedAt.UTC()})
		}
		return nil
	}, database.TxOptions{ReadOnly: true})
	return result, err
}

// Stats counts rows by publication state. Counting a large published backlog
// scans its partial index; prune published rows to keep this cheap.
type Stats struct {
	Pending   int64 `json:"pending"`
	Published int64 `json:"published"`
	Failed    int64 `json:"failed"`
}

func (p *Publisher) Stats(ctx context.Context) (Stats, error) {
	if p == nil || p.writer == nil || ctx == nil {
		return Stats{}, fault.New(fault.Invalid, "outbox stats require an initialized publisher and context")
	}
	fields := outboxstore.MessageFields()
	var stats Stats
	err := p.writer.Transaction(ctx, func(tx *database.Tx) error {
		for _, item := range []struct {
			state  outbox.PublicationState
			target *int64
		}{{outbox.Pending, &stats.Pending}, {outbox.Published, &stats.Published}, {outbox.PublicationFailed, &stats.Failed}} {
			count, err := outboxstore.QueryFoundryOutbox().Where(fields.PublishState.Eq(item.state)).Count(ctx, tx)
			if err != nil {
				return err
			}
			*item.target = count
		}
		return nil
	}, database.TxOptions{ReadOnly: true})
	return stats, err
}
