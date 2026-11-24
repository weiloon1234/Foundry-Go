package modelguide

import (
	"context"
	"strings"
	"time"

	"foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

//foundry:model table=guide_member_archives var=Archives key=ID
type ArchivedMember struct {
	ID        int64  `foundry:"member=ID,key-role=primary"`
	Name      string `foundry:"input=string"`
	Body      []byte
	CreatedAt time.Time `foundry:"role=created-at"`
	UpdatedAt time.Time `foundry:"role=updated-at"`
}

func (ArchivedMember) MutateName(name string) (string, error) {
	return strings.TrimSpace(name), nil
}

func CopyMembers(ctx context.Context, tx *database.Tx) (int64, error) {
	source, destination := hooks.Members, Archives
	return rows.InsertFrom(rows.From(source.Query()), destination.Query(),
		rows.MapInsert(destination.ID, expr.Value(source.Key())),
		rows.MapInsert(destination.Body, expr.Value(source.Body)),
		rows.MapInsert(destination.CreatedAt, expr.Value(source.CreatedAt))).
		Values(orm.Set(destination.Name, "copy")).Exec(ctx, tx)
}
