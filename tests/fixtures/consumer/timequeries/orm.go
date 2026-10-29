package timequeries

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"time"
)

// MemberTable declares stored columns and their write roles. Example: orm.KeyEq(Members.Key(), key).
type MemberTable struct {
	orm.Table[Member, model.ID[Member]]
	Name      orm.InputText[Member, string, string]
	CreatedAt orm.CreatedAt[Member, time.Time]
	UpdatedAt orm.UpdatedAt[Member, temporal.DateTime]
}

// Members preserves the persisted time_members model. Example: orm.All(ctx, db, Members.Query()).
var Members = declareMembers()

func declareMembers() MemberTable {
	var fields MemberTable
	fields = orm.Define("time_members", func(b orm.Builder[Member], m *Member) MemberTable {
		return MemberTable{Table: orm.TableOf(b, &m.ID),
			Name:      orm.InputTextOf[Member, string](b, &m.Name, nil),
			CreatedAt: orm.CreatedAtOf(b, &m.CreatedAt),
			UpdatedAt: orm.UpdatedAtOf(b, &m.UpdatedAt),
		}
	}, orm.WithHooks(func() orm.Hooks[Member] { return ormMemberHooks(fields) }))
	return fields
}

// ManualTable declares stored columns and their write roles. Example: orm.KeyEq(Manuals.Key(), key).
type ManualTable struct {
	orm.Table[Manual, model.ID[Manual]]
	CreatedAt orm.Time[Manual, time.Time]
	UpdatedAt orm.Time[Manual, temporal.DateTime]
}

// Manuals preserves the persisted time_manual model. Example: orm.All(ctx, db, Manuals.Query()).
var Manuals = orm.Define("time_manual", func(b orm.Builder[Manual], m *Manual) ManualTable {
	return ManualTable{Table: orm.TableOf(b, &m.ID),
		CreatedAt: orm.TimeOf(b, &m.CreatedAt),
		UpdatedAt: orm.TimeOf(b, &m.UpdatedAt),
	}
}, orm.NoTimestamps())
