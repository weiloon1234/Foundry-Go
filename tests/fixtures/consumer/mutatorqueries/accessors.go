package mutatorqueries

import (
	"errors"
	"strings"

	"github.com/weiloon1234/Foundry-Go/value"
)

// DisplayEmail is the explicitly selected presentation value of an email.
type DisplayEmail string

// DisplayNickname is a nickname prepared for this consumer's response.
type DisplayNickname string

// HiddenEmail reports that this consumer does not expose the email value.
var HiddenEmail = errors.New("email is not available for presentation")

// AccessID returns the transport spelling of the stored, model-owned ID.
// The ID field remains suitable for typed queries and relationships.
func (m Member) AccessID() (string, error) {
	return m.ID.String(), nil
}

// AccessEmail returns this consumer's presentation value without changing
// Email. It is an explicit call; hydration and JSON encoding do not call it.
func (m Member) AccessEmail() (DisplayEmail, error) {
	if m.Email == "hidden@example.test" {
		return "", HiddenEmail
	}
	return DisplayEmail(strings.ToUpper(m.Email)), nil
}

// AccessNickname preserves NULL separately from a present empty nickname.
func (m Member) AccessNickname() (value.Nullable[DisplayNickname], error) {
	text, present := m.Nickname.Get()
	if !present {
		return value.Null[DisplayNickname](), nil
	}
	return value.Of(DisplayNickname(strings.ToUpper(text))), nil
}
