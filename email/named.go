package email

import (
	"github.com/weiloon1234/Foundry-Go/internal/namedservice"
)

// MailerName selects an instance, distinct from names of other service families.
type MailerName string

// Validate validates the mailer name.
func (n MailerName) Validate() error { return namedservice.Validate(string(n)) }

// NamedMailer borrows one existing instance. Selection never creates another owner.
type NamedMailer struct {
	Name  MailerName
	Value *Mailer
}

// Mailers is immutable; default and named selection alias the same instance.
type Mailers struct {
	registry *namedservice.Registry[MailerName, Mailer]
}

// NewMailers constructs a named mailer registry with an explicit default selection.
func NewMailers(selected MailerName, entries ...NamedMailer) (*Mailers, error) {
	values := make([]namedservice.Entry[MailerName, Mailer], len(entries))
	for i, e := range entries {
		values[i] = namedservice.Entry[MailerName, Mailer]{Name: e.Name, Value: e.Value}
	}
	r, err := namedservice.New(selected, values)
	if err != nil {
		return nil, err
	}
	return &Mailers{registry: r}, nil
}

// Mailer looks up a named mailer through the registry.
func (r *Mailers) Mailer(name MailerName) (*Mailer, error) {
	if r == nil {
		return (*namedservice.Registry[MailerName, Mailer])(nil).Get(name)
	}
	return r.registry.Get(name)
}

// Default returns the selected default mailer or a registry error.
func (r *Mailers) Default() (*Mailer, error) {
	if r == nil {
		return (*namedservice.Registry[MailerName, Mailer])(nil).Default()
	}
	return r.registry.Default()
}

// Names returns the registry's mailer names, or nil for a nil registry.
func (r *Mailers) Names() []MailerName {
	if r == nil {
		return nil
	}
	return r.registry.Names()
}

// DefaultName returns the selected default name, or an empty name for a nil registry.
func (r *Mailers) DefaultName() MailerName {
	if r == nil {
		return ""
	}
	return r.registry.DefaultName()
}
