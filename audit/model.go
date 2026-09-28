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
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// ModelEntry owns an audit-row ID for one model; it is not a stored model row.
type ModelEntry[M any] struct{ _ [0]*M }
type ModelID[M any] = model.ID[ModelEntry[M]]

// ModelRecord contains complete audit representations, not a partial model.
type ModelRecord[M, K any] struct {
	id        ModelID[M]
	changes   record.Model[M, K]
	area      Area
	origin    attribution.Origin
	createdAt temporal.DateTime
}

func (r ModelRecord[M, K]) ID() ModelID[M]               { return r.id }
func (r ModelRecord[M, K]) Changes() record.Model[M, K]  { return r.changes }
func (r ModelRecord[M, K]) Area() Area                   { return r.area }
func (r ModelRecord[M, K]) Origin() attribution.Origin   { return r.origin }
func (r ModelRecord[M, K]) CreatedAt() temporal.DateTime { return r.createdAt }
func (ModelRecord[M, K]) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("model audit record"))
}

// RecordModel implements the generated observer boundary. Captured stored
// changes are validated before any SQL. Failure rolls back the model write.
func (r *Recorder) RecordModel(ctx context.Context, tx *database.Tx, entry record.Entry) error {
	area, err := r.area(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return fault.New(fault.Invalid, "audit recording requires a transaction")
	}
	if err := entry.Validate(); err != nil {
		return err
	}
	payload, err := entry.Payload()
	if err != nil {
		return err
	}
	draft, err := capturedDraft(area, attribution.FromContext(ctx), payload, value.Of(entry.Identity()))
	if err != nil {
		return err
	}
	_, err = auditstore.Append(ctx, tx, draft.SetOperation(entry.Operation()).SetAction("").SetVersion(0).SetRedacted(false))
	return err
}

// ModelHistory reads one stored subject's lifecycle history in the selected
// area. It does not fetch the subject model or require that it still exists.
// Page count and rows share query.Paginate's transaction/isolation contract.
func ModelHistory[M, K any](ctx context.Context, executor database.Executor, recorder *Recorder, subject model.Reference[M, K], request query.PageRequest) (query.Page[ModelRecord[M, K]], error) {
	area, err := recorder.area(ctx)
	if err != nil {
		return query.Page[ModelRecord[M, K]]{}, err
	}
	identity, err := subject.Identity()
	if err != nil {
		return query.Page[ModelRecord[M, K]]{}, err
	}
	key, err := value.NewJSON(identity)
	if err != nil {
		return query.Page[ModelRecord[M, K]]{}, err
	}
	f := auditstore.EntryFields()
	page, err := auditstore.Page(ctx, executor, auditstore.QueryFoundryAudit().Where(f.Area.Eq(string(area)), f.Subject.Eq(key), f.Operation.Ne(0)), request)
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
	changes, err := record.RestoreModel(template, entry)
	if err != nil {
		return ModelRecord[M, K]{}, err
	}
	origin, err := row.Origin.Decode()
	if err != nil {
		return ModelRecord[M, K]{}, err
	}
	if err := origin.Validate(); err != nil {
		return ModelRecord[M, K]{}, err
	}
	if err := Area(row.Area).Validate(); err != nil {
		return ModelRecord[M, K]{}, err
	}
	return ModelRecord[M, K]{id: model.IDFromBytes[ModelEntry[M]](row.ID.Bytes()), changes: changes, area: Area(row.Area), origin: origin, createdAt: row.CreatedAt}, nil
}

func capturedDraft(area Area, origin attribution.Origin, payload string, subject value.Nullable[model.Identity]) (auditstore.EntryDraft, error) {
	if err := origin.Validate(); err != nil {
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
	draft := auditstore.EntryDraft{}.SetArea(string(area)).SetPayload(body).SetOrigin(source)
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
