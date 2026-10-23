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
	p := orm.OnConflict(orm.ConflictKey(t.Key())).DoUpdate(orm.ConflictIncoming(t.Name), orm.ConflictSet(t.Note, coremodels.LabelInput{Text: "label"})).Where(t.Name.Ne("locked"))
	return orm.Upsert(ctx, db, t.Query(), p, t.Key().Set(1), t.Name.Set("name"), t.Body.Set([]byte{1}))
}
func Many(ctx context.Context, db database.Transactor) ([]h.Member, error) {
	t := h.Members
	p := orm.OnConflictConstraint[h.Member]("hook_members_pkey").DoNothing()
	return orm.UpsertMany(ctx, db, t.Query(), p, [][]orm.Assignment[h.Member]{{t.Key().Set(2), t.Body.Set([]byte{1})}})
}
