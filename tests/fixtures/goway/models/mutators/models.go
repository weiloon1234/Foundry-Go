package mutators

import (
	"foundry.test/goway/coremodels"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Member exercises typed input binding and explicit accessors.
type Member struct {
	ID      int64
	Email   coremodels.StoredEmail `foundry:"column=contact_email"`
	Note    value.Nullable[coremodels.StoredLabel]
	Name    string `foundry:"default=database"`
	Body    coremodels.StoredBody
	Enabled bool `foundry:"default=database"`
}

func (Member) MutateEmail(v coremodels.EmailInput) (coremodels.StoredEmail, error) {
	return coremodels.NormalizeEmail(v)
}
func (Member) MutateNote(v coremodels.LabelInput) (coremodels.StoredLabel, error) {
	return coremodels.NormalizeLabel(v)
}
func (Member) MutateName(v string) (string, error) { return coremodels.NormalizeName(v) }
func (Member) MutateBody(v coremodels.BinaryInput) (coremodels.StoredBody, error) {
	return coremodels.NormalizeBody(v)
}
func (m Member) AccessEmail() (coremodels.EmailLink, error) {
	return coremodels.EmailLink("mailto:" + string(m.Email)), nil
}
func (m Member) AccessNote() (value.Nullable[coremodels.DisplayLabel], error) {
	return coremodels.DisplayNote(m.Note)
}

type MemberTable struct {
	orm.Table[Member, int64]
	Email   orm.InputText[Member, coremodels.EmailInput, coremodels.StoredEmail]
	Note    orm.NullableInputText[Member, coremodels.LabelInput, coremodels.StoredLabel]
	Name    orm.Text[Member, string]
	Body    orm.InputColumn[Member, coremodels.BinaryInput, coremodels.StoredBody]
	Enabled orm.Column[Member, bool]
}

var Members = orm.Define("mutator_members", func(b orm.Builder[Member], m *Member) MemberTable {
	return MemberTable{
		Table:   orm.TableOf(b, &m.ID),
		Email:   orm.InputTextOf[Member, coremodels.EmailInput](b, &m.Email, nil),
		Note:    orm.NullableInputTextOf[Member, coremodels.LabelInput](b, &m.Note, nil),
		Name:    orm.TextOf(b, &m.Name),
		Body:    orm.InputBytesOf[Member, coremodels.BinaryInput](b, &m.Body, nil),
		Enabled: orm.ColumnOf(b, &m.Enabled),
	}
})
