// Package inputqueries exercises distinct mutation inputs through generated APIs.
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

//foundry:model table=input_members hooks=memberHooks
type Member struct {
	ID model.ID[Member]
	// Foundry field behavior (generated): Member.Email retains stored input_members.contact_email. Custom getter: [Member.AccessEmail]; choose the stored field or getter result explicitly when mapping a DTO.
	// Foundry field behavior (generated): Member.Email retains stored input_members.contact_email. Custom setter: [Member.MutateEmail] transforms assigned values during persistence through [MemberDraft.SetEmail]. Direct field assignment and draft construction do not invoke it.
	// Foundry field behavior (generated): The custom setter accepts inputqueries.EmailInput and produces stored inputqueries.StoredEmail; pass fresh input to the draft/conflict setter and use stored values for query comparisons.
	Email StoredEmail `foundry:"column=contact_email"`
	// Foundry field behavior (generated): Member.Note retains stored input_members.note. Custom setter: [Member.MutateNote] transforms assigned values during persistence through [MemberDraft.SetNote]. Direct field assignment and draft construction do not invoke it.
	// Foundry field behavior (generated): The custom setter accepts inputqueries.LabelInput and produces stored inputqueries.StoredLabel; pass fresh input to the draft/conflict setter and use stored values for query comparisons.
	// Foundry field behavior (generated): The write mutator skips omitted values and explicit SQL NULL; an assigned scalar zero value still invokes it.
	Note value.Nullable[StoredLabel]
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
