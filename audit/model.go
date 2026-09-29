package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/auditstore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ModelEntry owns an audit-row ID for one model; it is not a stored model row.
type ModelEntry[M any] struct{ _ [0]*M }
type ModelID[M any] = model.ID[ModelEntry[M]]

// ModelRecord contains complete audit representations, not a partial model.
// Its embedded metadata supplies Sequence, Area, Origin, Correlation, Route
// and CreatedAt.
type ModelRecord[M, K any] struct {
	metadata
	id      ModelID[M]
	changes record.Model[M, K]
}

func (r ModelRecord[M, K]) ID() ModelID[M]              { return r.id }
func (r ModelRecord[M, K]) Changes() record.Model[M, K] { return r.changes }
func (ModelRecord[M, K]) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("model audit record"))
}

// RecordModel implements the generated observer boundary. Captured stored
// changes are validated before any SQL. Values above the configured
// MaxValueBytes are stored as digest markers; a large payload never fails the
// write. Other failures roll back the model write.
func (r *Recorder) RecordModel(ctx context.Context, tx *database.Tx, entry record.Entry) error {
	area, err := r.area(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return fault.New(fault.Invalid, "audit recording requires a transaction")
	}
	entry, err = entry.Compact(r.config.valueLimit())
	if err != nil {
		return err
	}
	payload, err := entry.Payload()
	if err != nil {
		return err
	}
	draft, err := capturedDraft(ctx, area, payload, value.Of(entry.Identity()), entry.Redaction())
	if err != nil {
		return err
	}
	_, err = auditstore.Append(ctx, tx, draft.SetOperation(entry.Operation()).SetAction("").SetVersion(0).SetRedacted(false))
	return err
}

// ModelHistory reads one stored subject's lifecycle history in the selected
// area, newest first by insertion sequence. It does not fetch the subject model
// or require that it still exists. Page count and rows share query.Paginate's
// transaction/isolation contract; use ModelTimeline for keyset pages.
func ModelHistory[M, K any](ctx context.Context, executor database.Executor, recorder *Recorder, subject model.Reference[M, K], request query.PageRequest) (query.Page[ModelRecord[M, K]], error) {
	q, err := subjectHistory(ctx, recorder, subject)
	if err != nil {
		return query.Page[ModelRecord[M, K]]{}, err
	}
	f := auditstore.EntryFields()
	page, err := auditstore.Page(ctx, executor, q.Where(f.Operation.Ne(0)), request)
	if err != nil {
		return query.Page[ModelRecord[M, K]]{}, err
	}
	result := query.Page[ModelRecord[M, K]]{Number: page.Number, Size: page.Size, Total: page.Total, Pages: page.Pages, Items: make([]ModelRecord[M, K], 0, len(page.Items))}
	for _, row := range page.Items {
		if err := ctx.Err(); err != nil {
			return query.Page[ModelRecord[M, K]]{}, err
		}
		item, err := restoreModel(subject, row)
		if err != nil {
			return query.Page[ModelRecord[M, K]]{}, err
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

// ModelTimeline reads one subject's lifecycle history newest first using the
// indexed insertion sequence. It performs one bounded query without a count;
// pass Next as Before to continue. Concurrent writes do not form a snapshot.
func ModelTimeline[M, K any](ctx context.Context, executor database.Executor, recorder *Recorder, subject model.Reference[M, K], request TimelineRequest) (Timeline[ModelRecord[M, K]], error) {
	q, err := subjectHistory(ctx, recorder, subject)
	if err != nil {
		return Timeline[ModelRecord[M, K]]{}, err
	}
	f := auditstore.EntryFields()
	return readTimeline(ctx, executor, q.Where(f.Operation.Ne(0)), request, func(row auditstore.Entry) (ModelRecord[M, K], error) {
		return restoreModel(subject, row)
	})
}

// subjectHistory selects rows for one stored subject through its indexed
// lookup key and exact identity, in the selected area.
func subjectHistory[M, K any](ctx context.Context, recorder *Recorder, subject model.Reference[M, K]) (auditstore.EntryQuery, error) {
	area, err := recorder.area(ctx)
	if err != nil {
		return auditstore.EntryQuery{}, err
	}
	identity, err := subject.Identity()
	if err != nil {
		return auditstore.EntryQuery{}, err
	}
	key, err := identityKey(identity)
	if err != nil {
		return auditstore.EntryQuery{}, err
	}
	stored, err := value.NewJSON(identity)
	if err != nil {
		return auditstore.EntryQuery{}, err
	}
	f := auditstore.EntryFields()
	return auditstore.QueryFoundryAudit().Where(f.Area.Eq(string(area)), f.SubjectKey.Eq(key), f.Subject.Eq(stored)), nil
}

func restoreModel[M, K any](template model.Reference[M, K], row auditstore.Entry) (ModelRecord[M, K], error) {
	if row.Action != "" || row.Version != 0 || row.Redacted {
		return ModelRecord[M, K]{}, fault.New(fault.Invalid, "invalid stored model audit metadata")
	}
	subject, present := row.Subject.Get()
	if !present {
		return ModelRecord[M, K]{}, fault.New(fault.Invalid, "model audit has no subject")
	}
	identity, err := subject.Decode()
	if err != nil {
		return ModelRecord[M, K]{}, err
	}
	text, err := row.Payload.Text()
	if err != nil {
		return ModelRecord[M, K]{}, err
	}
	entry, err := record.ParseEntry(identity, row.Operation, text)
	if err != nil {
		return ModelRecord[M, K]{}, err
	}
	if uint8(entry.Redaction()) != row.Redaction {
		return ModelRecord[M, K]{}, fault.New(fault.Invalid, "stored model audit redaction policy does not match its payload")
	}
	changes, err := record.RestoreModel(template, entry)
	if err != nil {
		return ModelRecord[M, K]{}, err
	}
	meta, err := entryMetadata(row)
	if err != nil {
		return ModelRecord[M, K]{}, err
	}
	return ModelRecord[M, K]{metadata: meta, id: model.IDFromBytes[ModelEntry[M]](row.ID.Bytes()), changes: changes}, nil
}

// capturedDraft binds validated payload text with the context's attribution,
// correlation and matched route. The redaction policy is stored per row.
func capturedDraft(ctx context.Context, area Area, payload string, subject value.Nullable[model.Identity], redaction record.Redaction) (auditstore.EntryDraft, error) {
	origin := attribution.FromContext(ctx)
	if err := origin.Validate(); err != nil {
		return auditstore.EntryDraft{}, err
	}
	if err := redaction.Validate(); err != nil {
		return auditstore.EntryDraft{}, err
	}
	body, err := value.ParseJSON[json.RawMessage](payload)
	if err != nil {
		return auditstore.EntryDraft{}, err
	}
	source, err := value.NewJSON(origin)
	if err != nil {
		return auditstore.EntryDraft{}, err
	}
	draft := auditstore.EntryDraft{}.SetArea(string(area)).SetPayload(body).SetOrigin(source).SetRedaction(uint8(redaction))
	if correlation, present := CorrelationFromContext(ctx).Get(); present {
		if err := correlation.Validate(); err != nil {
			return auditstore.EntryDraft{}, err
		}
		draft = draft.SetCorrelation(string(correlation))
	} else {
		draft = draft.ClearCorrelation()
	}
	route, _ := attribution.RouteFromContext(ctx)
	draft = draft.SetRequestMethod(route.Method).SetRequestRoute(route.Name)
	if identity, present := subject.Get(); present {
		if err := identity.Validate(); err != nil {
			return auditstore.EntryDraft{}, err
		}
		key, err := value.NewJSON(identity)
		if err != nil {
			return auditstore.EntryDraft{}, err
		}
		return draft.SetSubject(key), nil
	}
	return draft.ClearSubject(), nil
}
