package conflictprobe

import (
	"context"
	"foundry.test/goway/coremodels"
	h "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

func One(ctx context.Context, db database.Transactor) (value.Optional[h.Member], error) {
	t := h.Members
	p := orm.OnConflict(orm.ConflictKey(orm.KeyExpr(t.Key()))).DoUpdate(orm.ConflictIncoming(orm.InputTextExpr(t.Name)), orm.ConflictSet(orm.NullableInputTextExpr(t.Note), coremodels.LabelInput{Text: "label"})).Where(orm.InputTextNe(t.Name, "locked"))
	return orm.Upsert(ctx, db, t.Query(), p, orm.KeySet(t.Key(), 1), orm.InputTextSet(t.Name, "name"), orm.ColumnSet(t.Body, []byte{1}))
}
func Many(ctx context.Context, db database.Transactor) ([]h.Member, error) {
	t := h.Members
	p := orm.OnConflictConstraint[h.Member]("hook_members_pkey").DoNothing()
	return orm.UpsertMany(ctx, db, t.Query(), p, [][]orm.Assignment[h.Member]{{orm.KeySet(t.Key(), 2), orm.ColumnSet(t.Body, []byte{1})}})
}
