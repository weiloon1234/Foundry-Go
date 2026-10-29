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

type Member struct {
	// ID is the database identity. Stored reads keep the key; call AccessID explicitly for presentation.
	// Custom getter [Member.AccessID] is explicit; persistence uses the stored identity.
	ID model.ID[Member]
	// Delivery address. Email retains the stored email_address value.
	// Email is the delivery address stored in email_address. Call AccessEmail explicitly for display.
	// MutateEmail runs during persistence through orm.InputTextSet(Members.Email, value); assigning this field does not invoke it.
	// Custom getter [Member.AccessEmail] is explicit when mapping a DTO.
	// Custom setter [Member.MutateEmail] transforms supplied values during persistence.
	// Direct field assignment and input construction do not invoke it.
	Email string `foundry:"column=email_address"`
	// Nickname retains the stored nullable value. Custom getter [Member.AccessNickname]
	// is explicit when mapping a DTO. Custom setter [Member.MutateNickname] runs during
	// persistence, skipping omitted values and SQL NULL; a supplied scalar zero invokes it.
	Nickname value.Nullable[string]
	// Attempts stores the persisted count. MutateAttempts runs during persistence through orm.InputNumberSet(Members.Attempts, value).
	// Direct assignment does not run the mutator.
	// Attempts retains the stored count. Custom setter [Member.MutateAttempts] runs
	// during persistence. Direct field assignment and input construction do not invoke it.
	Attempts int `foundry:"default=database"`
	// State retains the stored enum. Custom setter [Member.MutateState] runs during
	// persistence. Direct field assignment and input construction do not invoke it.
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
