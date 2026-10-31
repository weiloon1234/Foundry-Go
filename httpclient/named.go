package httpclient

import (
	"github.com/weiloon1234/Foundry-Go/internal/namedservice"
)

// Validate checks that the client name is a valid service identifier.
func (n Name) Validate() error { return namedservice.Validate(string(n)) }

// NamedClient borrows one existing instance. Selection never creates another owner.
type NamedClient struct {
	Name  Name
	Value *Client
}

// Clients is immutable; default and named selection alias the same instance.
type Clients struct {
	registry *namedservice.Registry[Name, Client]
}

// NewClients builds a named registry whose default aliases the selected client.
func NewClients(selected Name, entries ...NamedClient) (*Clients, error) {
	values := make([]namedservice.Entry[Name, Client], len(entries))
	for i, e := range entries {
		values[i] = namedservice.Entry[Name, Client]{Name: e.Name, Value: e.Value}
	}
	r, err := namedservice.New(selected, values)
	if err != nil {
		return nil, err
	}
	return &Clients{registry: r}, nil
}

// Client looks up the explicit name and returns an error if it is unavailable.
func (r *Clients) Client(name Name) (*Client, error) {
	if r == nil {
		return (*namedservice.Registry[Name, Client])(nil).Get(name)
	}
	return r.registry.Get(name)
}

// Default returns the selected client or an error if no default is available.
func (r *Clients) Default() (*Client, error) {
	if r == nil {
		return (*namedservice.Registry[Name, Client])(nil).Default()
	}
	return r.registry.Default()
}

// Names returns an owned snapshot of the registered names.
func (r *Clients) Names() []Name {
	if r == nil {
		return nil
	}
	return r.registry.Names()
}

// DefaultName returns the selected name, or an empty name for a nil registry.
func (r *Clients) DefaultName() Name {
	if r == nil {
		return ""
	}
	return r.registry.DefaultName()
}
