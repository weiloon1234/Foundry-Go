package audit

import (
	"context"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/auditstore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

func ParseModelID[M any](text string) (ModelID[M], error) { return model.ParseID[ModelEntry[M]](text) }
func ParseActionID[P any](text string) (ActionID[P], error) {
	return model.ParseID[ActionEntry[P]](text)
}

// FindModel reads an audit-row ID owned by the expected model. Supply a generated
// zero model's FoundryReference as the decoding template; its current key is not
// used. Foreign models/areas are absent, and no subject-model lookup occurs.
func FindModel[M, K any](ctx context.Context, executor database.Executor, recorder *Recorder, template model.Reference[M, K], id ModelID[M]) (value.Optional[ModelRecord[M, K]], error) {
	area, err := recorder.area(ctx)
	if err != nil {
		return value.Optional[ModelRecord[M, K]]{}, err
	}
	if id.IsZero() {
		return value.Optional[ModelRecord[M, K]]{}, fault.New(fault.Invalid, "model audit lookup requires a nonzero ID")
	}
	f := auditstore.EntryFields()
	selected, err := auditstore.QueryFoundryAudit().Where(f.Area.Eq(string(area)), f.Operation.Ne(0)).Find(ctx, executor, model.IDFromBytes[auditstore.Entry](id.Bytes()))
	if err != nil {
		return value.Optional[ModelRecord[M, K]]{}, err
	}
	row, present := selected.Get()
	if !present {
		return value.Optional[ModelRecord[M, K]]{}, nil
	}
	subject, present := row.Subject.Get()
	if !present {
		return value.Optional[ModelRecord[M, K]]{}, fault.New(fault.Invalid, "model audit has no subject")
	}
	identity, err := subject.Decode()
	if err != nil {
		return value.Optional[ModelRecord[M, K]]{}, err
	}
	if identity.ModelName() != template.ModelName() {
		return value.Optional[ModelRecord[M, K]]{}, nil
	}
	item, err := restoreModel(template, row)
	if err != nil {
		return value.Optional[ModelRecord[M, K]]{}, err
	}
	if err := ctx.Err(); err != nil {
		return value.Optional[ModelRecord[M, K]]{}, err
	}
	return value.Set(item), nil
}
