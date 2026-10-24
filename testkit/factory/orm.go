package factory

import (
	"context"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"slices"
)

// Define builds an ORM factory from an explicit table and typed assignments.
// States, composition and after-create hooks share the ordinary factory contract.
// Example: factory.Define(Users.Table,func(ctx context.Context,n factory.Sequence)([]orm.Assignment[User],error){return []orm.Assignment[User]{Users.Name.Set("Ada")},nil}).
func Define[M any, K comparable](table orm.Table[M, K], build Build[[]orm.Assignment[M]]) (*Factory[M, []orm.Assignment[M]], error) {
	if build == nil {
		return nil, fault.New(fault.Invalid, "factory requires a typed draft builder")
	}
	if err := orm.Check(table); err != nil {
		return nil, err
	}
	prepare := func(drafts [][]orm.Assignment[M], single bool) (func(context.Context, database.Transactor) ([]M, error), error) {
		rows := make([][]orm.Assignment[M], len(drafts))
		for i, draft := range drafts {
			rows[i] = slices.Clone(draft)
		}
		return func(ctx context.Context, writer database.Transactor) ([]M, error) {
			if single {
				created, err := table.Insert(ctx, writer, rows[0]...)
				if err != nil {
					return nil, err
				}
				return []M{created}, nil
			}
			return orm.InsertEach(ctx, writer, table, rows)
		}, nil
	}
	return &Factory[M, []orm.Assignment[M]]{prepare: prepare, build: build, sequence: &sequence{}}, nil
}
