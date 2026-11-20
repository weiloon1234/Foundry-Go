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
	p := orm.OnConflict(orm.ConflictKey(t.Key())).DoUpdate(orm.ConflictIncoming(t.Name), orm.ConflictSet(t.Note, coremodels.LabelInput{Text: "label"})).Where(orm.Ne(t.Name, "locked"))
	return orm.Upsert(ctx, db, t.Query(), p, orm.Set(t.Key(), 1), orm.Set(t.Name, "name"), orm.Set(t.Body, []byte{1}))
}
func Many(ctx context.Context, db database.Transactor) ([]h.Member, error) {
	t := h.Members
	p := orm.OnConflictConstraint[h.Member]("hook_members_pkey").DoNothing()
	return orm.UpsertMany(ctx, db, t.Query(), p, [][]orm.Assignment[h.Member]{{orm.Set(t.Key(), 2), orm.Set(t.Body, []byte{1})}})
}
