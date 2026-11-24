package pgsmoke_test

import (
	"foundry.test/goway/coremodels"
	hookmodels "foundry.test/goway/models/hooks"
	"github.com/weiloon1234/Foundry-Go/database/orm"
)

// declareHookMembers is identical in the goway and pgsmoke test packages.
func declareHookMembers(factory func() orm.Hooks[hookmodels.Member]) hookmodels.MemberTable {
	return orm.Define("hook_members", func(b orm.Builder[hookmodels.Member], m *hookmodels.Member) hookmodels.MemberTable {
		table := orm.TableOf(b, &m.ID)
		deleted := orm.SoftDeleteOf(b, &m.DeletedAt)
		return hookmodels.MemberTable{Table: table, ID: orm.PrimaryOf(b, table.Key()), Soft: orm.SoftDeletes(deleted), Name: orm.InputTextOf[hookmodels.Member, string](b, &m.Name, nil), Note: orm.NullableInputTextOf[hookmodels.Member, coremodels.LabelInput](b, &m.Note, nil), Body: orm.BytesOf(b, &m.Body), CreatedAt: orm.CreatedAtOf(b, &m.CreatedAt), UpdatedAt: orm.UpdatedAtOf(b, &m.UpdatedAt), DeletedAt: deleted}
	}, orm.WithHooks(factory))
}
