// Package mutatorqueries verifies automatic field behavior on handwritten models.
package mutatorqueries

import (
	"errors"
	"strings"

	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

//foundry:enum
type State string

const Active State = "active"

//foundry:model table=mutator_members
type Member struct {
	// Foundry field behavior (generated): Member.ID retains stored mutator_members.id. Custom getter: [Member.AccessID]; choose the stored field or getter result explicitly when mapping a DTO.
	// ID is the database identity.
	ID model.ID[Member]
	// Foundry field behavior (generated): Member.Email retains stored mutator_members.email_address. Custom getter: [Member.AccessEmail]; choose the stored field or getter result explicitly when mapping a DTO.
	// Foundry field behavior (generated): Member.Email retains stored mutator_members.email_address. Custom setter: [Member.MutateEmail] transforms assigned values during persistence through [MemberDraft.SetEmail]. Direct field assignment and draft construction do not invoke it.
	// Foundry field behavior (generated): Existing field documentation: Delivery address.
	Email string `foundry:"column=email_address"` // Delivery address.
	// Foundry field behavior (generated): Member.Nickname retains stored mutator_members.nickname. Custom getter: [Member.AccessNickname]; choose the stored field or getter result explicitly when mapping a DTO.
	// Foundry field behavior (generated): Member.Nickname retains stored mutator_members.nickname. Custom setter: [Member.MutateNickname] transforms assigned values during persistence through [MemberDraft.SetNickname]. Direct field assignment and draft construction do not invoke it.
	// Foundry field behavior (generated): The write mutator skips omitted values and explicit SQL NULL; an assigned scalar zero value still invokes it.
	Nickname value.Nullable[string]
	// Foundry field behavior (generated): Member.Attempts retains stored mutator_members.attempts. Custom setter: [Member.MutateAttempts] transforms assigned values during persistence through [MemberDraft.SetAttempts]. Direct field assignment and draft construction do not invoke it.
	Attempts int `foundry:"default=database"`
	// Foundry field behavior (generated): Member.State retains stored mutator_members.state. Custom setter: [Member.MutateState] transforms assigned values during persistence through [MemberDraft.SetState]. Direct field assignment and draft construction do not invoke it.
	State State `foundry:"default=database"`
}

var RejectedEmail = errors.New("email is rejected")

// MutateEmail transforms a supplied field value during persistence. The
// receiver is intentionally unused: this method does not read a model snapshot.
func (Member) MutateEmail(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	switch email {
	case "reject":
		return "", RejectedEmail
	case "panic":
		panic("injected field mutator failure")
	}
	return email, nil
}

func (Member) MutateNickname(nickname string) (string, error) { return nickname + "!", nil }
func (Member) MutateAttempts(attempts int) (int, error)       { return attempts + 1, nil }
func (Member) MutateState(state State) (State, error) {
	return State(strings.ToLower(strings.TrimSpace(string(state)))), nil
}
