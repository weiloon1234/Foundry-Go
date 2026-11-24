package mutators

import (
	"foundry.test/goway/coremodels"

	"github.com/weiloon1234/Foundry-Go/value"
)

// Member exercises typed input binding and explicit accessors.
//
//foundry:model table=mutator_members var=Members key=ID identity=false
type Member struct {
	ID      int64
	Email   coremodels.StoredEmail                 `foundry:"column=contact_email,input=coremodels.EmailInput"`
	Note    value.Nullable[coremodels.StoredLabel] `foundry:"input=coremodels.LabelInput"`
	Name    string                                 `foundry:"default=database,input=string"`
	Body    coremodels.StoredBody                  `foundry:"input=coremodels.BinaryInput"`
	Enabled bool                                   `foundry:"default=database,role=column"`
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
