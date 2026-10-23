package mutators

import (
	"foundry.test/goway/coremodels"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:model table=mutator_members primary=ID
type Member struct {
	ID int64
	// Foundry field behavior (generated): Member.Email retains stored mutator_members.contact_email. Custom getter: [Member.AccessEmail]; choose the stored field or getter result explicitly when mapping a DTO.
	// Foundry field behavior (generated): Member.Email retains stored mutator_members.contact_email. Custom setter: [Member.MutateEmail] transforms assigned values during persistence through [MemberDraft.SetEmail]. Direct field assignment and draft construction do not invoke it.
	// Foundry field behavior (generated): The custom setter accepts coremodels.EmailInput and produces stored coremodels.StoredEmail; pass fresh input to the draft/conflict setter and use stored values for query comparisons.
	Email coremodels.StoredEmail `foundry:"column=contact_email"`
	// Foundry field behavior (generated): Member.Note retains stored mutator_members.note. Custom getter: [Member.AccessNote]; choose the stored field or getter result explicitly when mapping a DTO.
	// Foundry field behavior (generated): Member.Note retains stored mutator_members.note. Custom setter: [Member.MutateNote] transforms assigned values during persistence through [MemberDraft.SetNote]. Direct field assignment and draft construction do not invoke it.
	// Foundry field behavior (generated): The custom setter accepts coremodels.LabelInput and produces stored coremodels.StoredLabel; pass fresh input to the draft/conflict setter and use stored values for query comparisons.
	// Foundry field behavior (generated): The write mutator skips omitted values and explicit SQL NULL; an assigned scalar zero value still invokes it.
	Note value.Nullable[coremodels.StoredLabel]
	// Foundry field behavior (generated): Member.Name retains stored mutator_members.name. Custom setter: [Member.MutateName] transforms assigned values during persistence through [MemberDraft.SetName]. Direct field assignment and draft construction do not invoke it.
	Name string `foundry:"default=database"`
	// Foundry field behavior (generated): Binary persistence uses bytea and owns byte buffers in drafts, query values and change snapshots. A non-nil empty slice is present; nil is invalid. Use Nullable and the generated Clear setter for SQL NULL. Binary fields cannot be identity or relation keys.
	// Foundry field behavior (generated): Member.Body retains stored mutator_members.body. Custom setter: [Member.MutateBody] transforms assigned values during persistence through [MemberDraft.SetBody]. Direct field assignment and draft construction do not invoke it.
	// Foundry field behavior (generated): The custom setter accepts coremodels.BinaryInput and produces stored coremodels.StoredBody; pass fresh input to the draft/conflict setter and use stored values for query comparisons.
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
