package sourcewrite

import (
	"context"
	m "foundry.test/goway/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	r "github.com/weiloon1234/Foundry-Go/database/orm/rows"
)

func Write(ctx context.Context, db database.Transactor) ([]m.WriteRecord, error) {
	f := m.WriteRecords
	source := r.From(f.Query().Limit(2))
	key := r.MatchSource(f.Key(), expr.Value(f.Key()))
	if _, err := r.InsertFrom(source, f.Query(), r.MapInsert(f.Name, expr.Value(f.Name)), r.MapInsert(f.Note, expr.Value(f.Note))).Exec(ctx, db); err != nil {
		return nil, err
	}
	if _, err := r.DeleteUsing(f.Query(), source).Match(key).Returning(ctx, db, 2); err != nil {
		return nil, err
	}
	return r.UpdateFrom(f.Query(), source).Match(key).Select(r.MapUpdate(f.Name, expr.Value(f.Name)), r.MapUpdate(f.Note, expr.Value(f.Note))).Returning(ctx, db, 2)
}
