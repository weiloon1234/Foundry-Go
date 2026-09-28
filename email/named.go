package email

import (
	"github.com/weiloon1234/Foundry-Go/internal/namedservice"
)

// MailerName selects an instance, distinct from names of other service families.
type MailerName string

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
func (r *Mailers) Mailer(name MailerName) (*Mailer, error) {
	if r == nil {
		return (*namedservice.Registry[MailerName, Mailer])(nil).Get(name)
	}
	return r.registry.Get(name)
}
func (r *Mailers) Default() (*Mailer, error) {
	if r == nil {
		return (*namedservice.Registry[MailerName, Mailer])(nil).Default()
	}
	return r.registry.Default()
}
func (r *Mailers) Names() []MailerName {
	if r == nil {
		return nil
	}
	return r.registry.Names()
}
func (r *Mailers) DefaultName() MailerName {
	if r == nil {
		return ""
	}
	return r.registry.DefaultName()
}
