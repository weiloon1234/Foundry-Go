package audit

import (
	"context"
	"fmt"
	"reflect"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/audit/record"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/auditstore"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type ActionName string
type Version uint32

// Action declares a versioned domain audit DTO. Declare and reuse one descriptor
// for each schema. Redaction never changes the input or silently hydrates a
// partially redacted DTO on subsequent reads.
type Action[P any] struct {
	_       [0]*P
	name    ActionName
	version Version
}

func Define[P any](name ActionName, version Version) Action[P] {
	return Action[P]{name: name, version: version}
}
func (a Action[P]) Name() ActionName { return a.name }
func (a Action[P]) Version() Version { return a.version }
func (a Action[P]) Validate() error {
	if !identifier.Semantic(string(a.name)) || a.version == 0 || reflect.TypeFor[P]().Kind() == reflect.Interface {
		return fault.New(fault.Invalid, "audit action requires a name, version and concrete payload type")
	}
	return nil
}

type ActionEntry[P any] struct{ _ [0]*P }
type ActionID[P any] = model.ID[ActionEntry[P]]

// Record captures a domain event without a model subject. It neither commits
// the outer transaction nor delivers a notification. Its ID is not a receipt.
func (a Action[P]) Record(ctx context.Context, tx *database.Tx, recorder *Recorder, payload P) (ActionID[P], error) {
	return a.record(ctx, tx, recorder, value.Nullable[model.Identity]{}, payload)
}

// RecordFor captures a concrete model-owned subject reference without another
// model lookup or read accessor. Origin/subject metadata is not authorization.
func RecordFor[M, K, P any](ctx context.Context, tx *database.Tx, recorder *Recorder, action Action[P], subject model.Reference[M, K], payload P) (ActionID[P], error) {
	if _, err := recorder.area(ctx); err != nil {
		return ActionID[P]{}, err
	}
	if tx == nil {
		return ActionID[P]{}, fault.New(fault.Invalid, "audit recording requires a transaction")
	}
	if err := action.Validate(); err != nil {
		return ActionID[P]{}, err
	}
	identity, err := subject.Identity()
	if err != nil {
		return ActionID[P]{}, err
	}
	return action.record(ctx, tx, recorder, value.Of(identity), payload)
}

func (a Action[P]) record(ctx context.Context, tx *database.Tx, recorder *Recorder, subject value.Nullable[model.Identity], payload P) (ActionID[P], error) {
	area, err := recorder.area(ctx)
	if err != nil {
		return ActionID[P]{}, err
	}
	if tx == nil {
		return ActionID[P]{}, fault.New(fault.Invalid, "audit recording requires a transaction")
	}
	if err := a.Validate(); err != nil {
		return ActionID[P]{}, err
	}
	origin := attribution.FromContext(ctx)
	if err := origin.Validate(); err != nil {
		return ActionID[P]{}, err
	}
	captured, err := record.CaptureDocument(payload)
	if err != nil {
		return ActionID[P]{}, err
	}
	text, err := captured.Payload()
	if err != nil {
		return ActionID[P]{}, err
	}
	draft, err := capturedDraft(ctx, area, text, subject, captured.Redaction())
	if err != nil {
		return ActionID[P]{}, err
	}
	row, err := auditstore.Append(ctx, tx, draft.SetOperation(0).SetAction(string(a.name)).SetVersion(uint32(a.version)).SetRedacted(captured.Redacted()))
	if err != nil {
		return ActionID[P]{}, err
	}
	return model.IDFromBytes[ActionEntry[P]](row.ID.Bytes()), nil
}

// ActionRecord is immutable persisted domain history. Document.Decode returns a
// fresh DTO only when the stored representation is complete and unredacted.
// Its embedded metadata supplies Sequence, Area, Origin, Correlation, Route and
// CreatedAt.
type ActionRecord[P any] struct {
	metadata
	id       ActionID[P]
	document record.Document[P]
	subject  value.Nullable[model.Identity]
}

func (r ActionRecord[P]) ID() ActionID[P]                         { return r.id }
func (r ActionRecord[P]) Document() record.Document[P]            { return r.document }
func (r ActionRecord[P]) Subject() value.Nullable[model.Identity] { return r.subject }
func (ActionRecord[P]) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("domain audit record"))
}

// Find selects only this action schema and area. It validates persisted DTO and
// redaction metadata again under the row's own redaction policy; a foreign
// action/version/area is absent.
func (a Action[P]) Find(ctx context.Context, executor database.Executor, recorder *Recorder, id ActionID[P]) (value.Optional[ActionRecord[P]], error) {
	q, err := a.history(ctx, recorder)
	if err != nil {
		return value.Optional[ActionRecord[P]]{}, err
	}
	if id.IsZero() {
		return value.Optional[ActionRecord[P]]{}, fault.New(fault.Invalid, "audit lookup requires a nonzero ID")
	}
	selected, err := q.Find(ctx, executor, model.IDFromBytes[auditstore.Entry](id.Bytes()))
	if err != nil {
		return value.Optional[ActionRecord[P]]{}, err
	}
	row, present := selected.Get()
	if !present {
		return value.Optional[ActionRecord[P]]{}, nil
	}
	result, err := restoreAction[P](row)
	if err != nil {
		return value.Optional[ActionRecord[P]]{}, err
	}
	if err := ctx.Err(); err != nil {
		return value.Optional[ActionRecord[P]]{}, err
	}
	return value.Set(result), nil
}

// Timeline lists this action name/version in the selected area, newest first.
func (a Action[P]) Timeline(ctx context.Context, executor database.Executor, recorder *Recorder, request TimelineRequest) (Timeline[ActionRecord[P]], error) {
	q, err := a.history(ctx, recorder)
	if err != nil {
		return Timeline[ActionRecord[P]]{}, err
	}
	return readTimeline(ctx, executor, q, request, restoreAction[P])
}

// ActionTimelineFor lists this action's records about one subject, newest
// first, using the indexed subject key and an exact identity comparison.
func ActionTimelineFor[M, K, P any](ctx context.Context, executor database.Executor, recorder *Recorder, action Action[P], subject model.Reference[M, K], request TimelineRequest) (Timeline[ActionRecord[P]], error) {
	if err := action.Validate(); err != nil {
		return Timeline[ActionRecord[P]]{}, err
	}
	q, err := subjectHistory(ctx, recorder, subject)
	if err != nil {
		return Timeline[ActionRecord[P]]{}, err
	}
	f := auditstore.EntryFields()
	return readTimeline(ctx, executor, q.Where(f.Action.Eq(string(action.name)), f.Version.Eq(uint32(action.version)), f.Operation.Eq(0)), request, restoreAction[P])
}

func (a Action[P]) history(ctx context.Context, recorder *Recorder) (auditstore.EntryQuery, error) {
	area, err := recorder.area(ctx)
	if err != nil {
		return auditstore.EntryQuery{}, err
	}
	if err := a.Validate(); err != nil {
		return auditstore.EntryQuery{}, err
	}
	f := auditstore.EntryFields()
	return auditstore.QueryFoundryAudit().Where(f.Area.Eq(string(area)), f.Action.Eq(string(a.name)), f.Version.Eq(uint32(a.version)), f.Operation.Eq(0)), nil
}

func restoreAction[P any](row auditstore.Entry) (ActionRecord[P], error) {
	text, err := row.Payload.Text()
	if err != nil {
		return ActionRecord[P]{}, err
	}
	document, err := record.ParseDocumentWith[P](record.Redaction(row.Redaction), text, row.Redacted)
	if err != nil {
		return ActionRecord[P]{}, err
	}
	meta, err := entryMetadata(row)
	if err != nil {
		return ActionRecord[P]{}, err
	}
	var subject value.Nullable[model.Identity]
	if captured, present := row.Subject.Get(); present {
		identity, err := captured.Decode()
		if err != nil {
			return ActionRecord[P]{}, err
		}
		if err := identity.Validate(); err != nil {
			return ActionRecord[P]{}, err
		}
		subject = value.Of(identity)
	}
	return ActionRecord[P]{metadata: meta, id: model.IDFromBytes[ActionEntry[P]](row.ID.Bytes()), document: document, subject: subject}, nil
}
