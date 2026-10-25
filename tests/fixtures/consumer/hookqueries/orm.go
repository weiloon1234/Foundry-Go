package hookqueries

import (
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/model"
)

// MemberTable declares stored columns and their write roles. Example: Members.Key().Eq(key).
type MemberTable struct {
	orm.Table[Member, model.ID[Member]]
	Email        orm.InputText[Member, string, string]
	Nickname     orm.NullableInputText[Member, string, string]
	IntroducerID orm.NullableColumn[Member, model.ID[Member]]
	IsIntroducer orm.Column[Member, bool]
	Rank         orm.Number[Member, int]
}

// Members preserves the persisted hook_members model. Example: Members.Query().All(ctx,db).
var Members = declareMembers()

func declareMembers() MemberTable {
	var fields MemberTable
	fields = orm.Define("hook_members", func(b orm.Builder[Member], m *Member) MemberTable {
		return MemberTable{Table: orm.TableOf(b, &m.ID),
			Email:        orm.InputTextOf[Member, string](b, &m.Email, nil),
			Nickname:     orm.NullableInputTextOf[Member, string](b, &m.Nickname, nil),
			IntroducerID: orm.NullableColumnOf(b, &m.IntroducerID),
			IsIntroducer: orm.ColumnOf(b, &m.IsIntroducer),
			Rank:         orm.NumberOf(b, &m.Rank),
		}
	}, orm.WithHooks(func() orm.Hooks[Member] { return ormMemberHooks(fields) }))
	return fields
}

// LogTable declares stored columns and their write roles. Example: Logs.Key().Eq(key).
type LogTable struct {
	orm.Table[Log, model.ID[Log]]
	Label orm.Text[Log, string]
}

// Logs preserves the persisted hook_logs model. Example: Logs.Query().All(ctx,db).
var Logs = orm.Define("hook_logs", func(b orm.Builder[Log], m *Log) LogTable {
	return LogTable{Table: orm.TableOf(b, &m.ID),
		Label: orm.TextOf(b, &m.Label),
	}
})
