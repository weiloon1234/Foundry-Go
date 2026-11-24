// Package inputqueries exercises distinct mutation inputs through typed ORM assignments.
package inputqueries

import (
	"errors"
	"strings"

	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

type EmailInput struct{ Address string }
type StoredEmail string
type EmailLink string
type LabelInput struct{ Text string }
type StoredLabel string

var Veto = errors.New("input fixture veto")

//foundry:model table=input_members var=Members key=ID identity=false hooks=memberHooks
type Member struct {
	ID model.ID[Member]
	// Email stores the normalized address in contact_email.
	// Custom setter: MutateEmail accepts EmailInput; Custom getter: AccessEmail returns EmailLink.
	Email StoredEmail                 `foundry:"column=contact_email,input=EmailInput"`
	Note  value.Nullable[StoredLabel] `foundry:"input=LabelInput"`
}

func (Member) MutateEmail(input EmailInput) (StoredEmail, error) {
	if input.Address == "reject" {
		return "", Veto
	}
	return StoredEmail(strings.ToLower(strings.TrimSpace(input.Address))), nil
}

func (Member) MutateNote(input LabelInput) (StoredLabel, error) {
	return StoredLabel(strings.TrimSpace(input.Text)), nil
}

func (member Member) AccessEmail() (EmailLink, error) {
	return EmailLink("mailto:" + string(member.Email)), nil
}
