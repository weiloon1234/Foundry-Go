package email

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/mail"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/weiloon1234/Foundry-Go/contract"
)

// Address is one validated ASCII mailbox with an optional UTF-8 display name.
// SMTPUTF8 local parts and IDN conversion are deliberately not implicit.
type Address struct{ mailbox, name string }

const MaxAddressBytes = 1024
const MaxMailboxBytes = 254
const MaxDisplayNameBytes = 256

func ParseAddress(text string) (Address, error) {
	if len(text) > MaxAddressBytes || !headerText(text) {
		return Address{}, Construction
	}
	parsed, err := mail.ParseAddress(text)
	if err != nil {
		return Address{}, Construction
	}
	return NewAddress(parsed.Address, parsed.Name)
}
func NewAddress(mailbox, name string) (Address, error) {
	if len(mailbox) > MaxMailboxBytes || len(name) > MaxDisplayNameBytes || !headerText(mailbox) || !headerText(name) {
		return Address{}, Construction
	}
	for _, c := range mailbox {
		if c < 33 || c > 126 {
			return Address{}, Construction
		}
	}
	parsed, err := mail.ParseAddress(mailbox)
	if err != nil || parsed.Address != mailbox || parsed.Name != "" {
		return Address{}, Construction
	}
	local, domain, ok := strings.Cut(mailbox, "@")
	if !ok || local == "" || domain == "" || len(local) > 64 || strings.Contains(domain, "@") {
		return Address{}, Construction
	}
	return Address{mailbox: mailbox, name: name}, nil
}
func (a Address) Validate() error { _, err := NewAddress(a.mailbox, a.name); return err }
func (a Address) Mailbox() string { return a.mailbox }
func (a Address) Name() string    { return a.name }

// Header explicitly reveals the formatted mailbox. Generic formatting is redacted.
func (a Address) Header() string           { return (&mail.Address{Address: a.mailbox, Name: a.name}).String() }
func (Address) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("email address")) }
func (Address) LogValue() slog.Value       { return slog.StringValue("email address") }
func (a Address) MarshalJSON() ([]byte, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(a.Header())
}
func (a *Address) UnmarshalJSON(data []byte) error {
	if a == nil || len(data) > MaxAddressBytes*6+2 {
		return Construction
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return Construction
	}
	parsed, err := ParseAddress(text)
	if err != nil {
		return err
	}
	*a = parsed
	return nil
}
func headerText(text string) bool {
	if !utf8.ValidString(text) {
		return false
	}
	for _, c := range text {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}

// JSONContract is the single scalar wire declaration used by generated DTOs and
// contract export. Display-name mailboxes are strings, not OpenAPI email-format
// strings; NewAddress/ParseAddress retain the stricter runtime mailbox checks.
func (Address) JSONContract() contract.JSON[Address] {
	typ := reflect.TypeFor[Address]()
	id := contract.TypeID(typ.PkgPath() + "." + typ.Name())
	return contract.DefineJSONValue[Address](contract.Schema{Root: id, Types: []contract.Type{{ID: id, Kind: contract.StringKind}}})
}

// Text configuration reuses the same mailbox parser as message construction.
func (a Address) MarshalText() ([]byte, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	return []byte(a.Header()), nil
}
func (a *Address) UnmarshalText(data []byte) error {
	if a == nil {
		return Construction
	}
	parsed, err := ParseAddress(string(data))
	if err != nil {
		return err
	}
	*a = parsed
	return nil
}
