package audit

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/auditstore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlvalue"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Sequence is the database-assigned insertion order of audit rows. It orders
// history correctly even when several rows share one transaction timestamp.
// It is a position for keyset reads, not an identifier or a count.
type Sequence int64

// TimelineRequest selects at most Size rows, newest first, strictly older than
// Before when it is set. Pass a returned Timeline.Next as Before to continue.
type TimelineRequest struct {
	Size   int
	Before value.Optional[Sequence]
}

func (r TimelineRequest) Validate() error {
	if r.Size < 1 || r.Size > query.MaxPageSize {
		return fault.New(fault.Invalid, "audit timeline size is outside its bounds")
	}
	if before, present := r.Before.Get(); present && before < 1 {
		return fault.New(fault.Invalid, "audit timeline position must be positive")
	}
	return nil
}

// Timeline is one keyset page. Next is set when older rows may follow; it is a
// position hint, not a snapshot across concurrent writes.
type Timeline[R any] struct {
	Items []R
	Next  value.Optional[Sequence]
}

// metadata is shared by typed records and activity summaries.
type metadata struct {
	sequence    Sequence
	area        Area
	origin      attribution.Origin
	correlation value.Optional[Correlation]
	route       value.Optional[attribution.Route]
	createdAt   temporal.DateTime
}

func (m metadata) Sequence() Sequence                       { return m.sequence }
func (m metadata) Area() Area                               { return m.area }
func (m metadata) Origin() attribution.Origin               { return m.origin }
func (m metadata) Correlation() value.Optional[Correlation] { return m.correlation }
func (m metadata) CreatedAt() temporal.DateTime             { return m.createdAt }

// Route returns the matched request route that wrote the row, when the write
// happened inside a routed request.
func (m metadata) Route() value.Optional[attribution.Route] { return m.route }

func entryMetadata(row auditstore.Entry) (metadata, error) {
	return decodeMetadata(row.Sequence, row.Area, row.Origin, row.Correlation, row.RequestMethod, row.RequestRoute, row.CreatedAt)
}

func decodeMetadata(sequence int64, area string, source value.JSON[attribution.Origin], correlation value.Nullable[string], method, route string, createdAt temporal.DateTime) (metadata, error) {
	result := metadata{sequence: Sequence(sequence), area: Area(area), createdAt: createdAt}
	if sequence < 1 {
		return metadata{}, fault.New(fault.Invalid, "stored audit sequence is invalid")
	}
	if err := result.area.Validate(); err != nil {
		return metadata{}, err
	}
	origin, err := source.Decode()
	if err != nil {
		return metadata{}, err
	}
	if err := origin.Validate(); err != nil {
		return metadata{}, err
	}
	result.origin = origin
	if text, present := correlation.Get(); present {
		if err := Correlation(text).Validate(); err != nil {
			return metadata{}, err
		}
		result.correlation = value.Set(Correlation(text))
	}
	if method != "" || route != "" {
		matched := attribution.Route{Method: method, Name: route}
		if err := matched.Validate(); err != nil {
			return metadata{}, err
		}
		result.route = value.Set(matched)
	}
	return result, nil
}

// Activity is a payload-free summary of one stored audit row for heterogeneous
// timelines: a subject's combined history, an actor's changes or everything
// written under one correlation. Load typed details with FindModel/Action.Find
// using ModelActivityID or ActionActivityID. Its embedded metadata supplies
// Sequence, Area, Origin, Correlation, Route and CreatedAt.
type Activity struct {
	metadata
	id        [16]byte
	operation lifecycle.Operation
	action    ActionName
	version   Version
	subject   value.Nullable[model.Identity]
}

// Operation is the model lifecycle operation, or zero for a domain action.
func (a Activity) Operation() lifecycle.Operation { return a.operation }

// Action returns the domain action name and version; both are zero for model rows.
func (a Activity) Action() (ActionName, Version)           { return a.action, a.version }
func (a Activity) IsAction() bool                          { return a.operation == 0 }
func (a Activity) Subject() value.Nullable[model.Identity] { return a.subject }
func (Activity) Format(state fmt.State, _ rune)            { _, _ = state.Write([]byte("audit activity")) }

// ModelActivityID converts a model activity into its typed audit ID when the
// row belongs to template's model. It performs no lookup or authorization.
func ModelActivityID[M, K any](activity Activity, template model.Reference[M, K]) (ModelID[M], bool) {
	identity, present := activity.subject.Get()
	if activity.IsAction() || !present || identity.ModelName() != template.ModelName() || activity.id == [16]byte{} {
		return ModelID[M]{}, false
	}
	return model.IDFromBytes[ModelEntry[M]](activity.id), true
}

// ActionActivityID converts a domain activity into its typed ID when the row
// was recorded by this action name and version.
func ActionActivityID[P any](activity Activity, action Action[P]) (ActionID[P], bool) {
	if !activity.IsAction() || activity.action != action.name || activity.version != action.version || activity.id == [16]byte{} {
		return ActionID[P]{}, false
	}
	return model.IDFromBytes[ActionEntry[P]](activity.id), true
}

func decodeActivity(row auditstore.Activity) (Activity, error) {
	meta, err := decodeMetadata(row.Sequence, row.Area, row.Origin, row.Correlation, row.RequestMethod, row.RequestRoute, row.CreatedAt)
	if err != nil {
		return Activity{}, err
	}
	result := Activity{metadata: meta, id: row.ID.Bytes(), operation: row.Operation, action: ActionName(row.Action), version: Version(row.Version)}
	if row.Operation == 0 && (row.Action == "" || row.Version == 0) || row.Operation != 0 && (row.Action != "" || row.Version != 0) {
		return Activity{}, fault.New(fault.Invalid, "invalid stored audit activity metadata")
	}
	if captured, present := row.Subject.Get(); present {
		identity, err := captured.Decode()
		if err != nil {
			return Activity{}, err
		}
		if err := identity.Validate(); err != nil {
			return Activity{}, err
		}
		result.subject = value.Of(identity)
	}
	return result, nil
}

// SubjectActivity lists model changes and domain actions recorded for one
// subject, newest first, without decoding their payloads.
func SubjectActivity[M, K any](ctx context.Context, executor database.Executor, recorder *Recorder, subject model.Reference[M, K], request TimelineRequest) (Timeline[Activity], error) {
	q, err := subjectHistory(ctx, recorder, subject)
	if err != nil {
		return Timeline[Activity]{}, err
	}
	return readActivity(ctx, executor, q, request, func(Activity) bool { return true })
}

// ActorActivity lists rows whose attribution names this model actor, newest
// first. The actor key is indexed; each row's origin is also compared exactly.
func ActorActivity[M, K any](ctx context.Context, executor database.Executor, recorder *Recorder, actor model.Reference[M, K], request TimelineRequest) (Timeline[Activity], error) {
	area, err := recorder.area(ctx)
	if err != nil {
		return Timeline[Activity]{}, err
	}
	identity, err := actor.Identity()
	if err != nil {
		return Timeline[Activity]{}, err
	}
	key, err := identityKey(identity)
	if err != nil {
		return Timeline[Activity]{}, err
	}
	f := auditstore.EntryFields()
	return readActivity(ctx, executor, auditstore.QueryFoundryAudit().Where(f.Area.Eq(string(area)), f.ActorKey.Eq(key)), request, func(item Activity) bool {
		stored, present := item.origin.Model()
		return present && stored == identity
	})
}

// SystemActivity lists rows attributed to one system identity, newest first.
func SystemActivity(ctx context.Context, executor database.Executor, recorder *Recorder, system attribution.SystemID, request TimelineRequest) (Timeline[Activity], error) {
	area, err := recorder.area(ctx)
	if err != nil {
		return Timeline[Activity]{}, err
	}
	origin, err := (attribution.Origin{}).WithSystem(system)
	if err != nil {
		return Timeline[Activity]{}, err
	}
	f := auditstore.EntryFields()
	return readActivity(ctx, executor, auditstore.QueryFoundryAudit().Where(f.Area.Eq(string(area)), f.ActorKey.Eq(textKey("s:"+string(origin.System())))), request, func(item Activity) bool {
		return item.origin.System() == system
	})
}

// CorrelationActivity lists every row written under one correlation, such as
// one request or explicit batch, newest first.
func CorrelationActivity(ctx context.Context, executor database.Executor, recorder *Recorder, correlation Correlation, request TimelineRequest) (Timeline[Activity], error) {
	area, err := recorder.area(ctx)
	if err != nil {
		return Timeline[Activity]{}, err
	}
	if err := correlation.Validate(); err != nil {
		return Timeline[Activity]{}, err
	}
	f := auditstore.EntryFields()
	return readActivity(ctx, executor, auditstore.QueryFoundryAudit().Where(f.Area.Eq(string(area)), f.Correlation.Eq(string(correlation))), request, func(Activity) bool { return true })
}

func readActivity(ctx context.Context, executor database.Executor, q auditstore.EntryQuery, request TimelineRequest, keep func(Activity) bool) (Timeline[Activity], error) {
	if err := request.Validate(); err != nil {
		return Timeline[Activity]{}, err
	}
	before, _ := request.Before.Get()
	rows, err := auditstore.RecentActivity(ctx, executor, q, int64(before), request.Size+1)
	if err != nil {
		return Timeline[Activity]{}, err
	}
	more := len(rows) > request.Size
	rows = rows[:min(len(rows), request.Size)]
	result := Timeline[Activity]{Items: make([]Activity, 0, len(rows))}
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return Timeline[Activity]{}, err
		}
		item, err := decodeActivity(row)
		if err != nil {
			return Timeline[Activity]{}, err
		}
		if keep(item) {
			result.Items = append(result.Items, item)
		}
	}
	if more && len(rows) > 0 {
		result.Next = value.Set(Sequence(rows[len(rows)-1].Sequence))
	}
	return result, nil
}

func readTimeline[R any](ctx context.Context, executor database.Executor, q auditstore.EntryQuery, request TimelineRequest, decode func(auditstore.Entry) (R, error)) (Timeline[R], error) {
	if err := request.Validate(); err != nil {
		return Timeline[R]{}, err
	}
	before, _ := request.Before.Get()
	rows, err := auditstore.Recent(ctx, executor, q, int64(before), request.Size+1)
	if err != nil {
		return Timeline[R]{}, err
	}
	more := len(rows) > request.Size
	rows = rows[:min(len(rows), request.Size)]
	result := Timeline[R]{Items: make([]R, 0, len(rows))}
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return Timeline[R]{}, err
		}
		item, err := decode(row)
		if err != nil {
			return Timeline[R]{}, err
		}
		result.Items = append(result.Items, item)
	}
	if more && len(rows) > 0 {
		result.Next = value.Set(Sequence(rows[len(rows)-1].Sequence))
	}
	return result, nil
}

// identityKey mirrors the generated subject_key/actor_key SQL expression. It is
// an index key; queries still compare stored identities exactly.
func identityKey(identity model.Identity) (string, error) {
	keyJSON, err := identity.KeyJSON()
	if err != nil {
		return "", err
	}
	var key sqlvalue.Value
	if err := json.Unmarshal([]byte(keyJSON), &key); err != nil {
		return "", fault.Wrap(fault.Invalid, "invalid audit subject key", err)
	}
	return textKey("m:" + identity.ModelName() + ":" + key.Kind + ":" + key.Text), nil
}

func textKey(text string) string {
	sum := md5.Sum([]byte(text))
	return hex.EncodeToString(sum[:])
}
