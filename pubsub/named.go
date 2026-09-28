package pubsub

import (
	"github.com/weiloon1234/Foundry-Go/internal/namedservice"
)

// ConnectionName selects an instance, distinct from names of other service families.
type ConnectionName string

func (n ConnectionName) Validate() error { return namedservice.Validate(string(n)) }

// NamedBroker borrows one existing instance. Selection never creates another owner.
type NamedBroker struct {
	Name  ConnectionName
	Value *Broker
}

// Brokers is immutable; default and named selection alias the same instance.
type Brokers struct {
	registry *namedservice.Registry[ConnectionName, Broker]
}

func NewBrokers(selected ConnectionName, entries ...NamedBroker) (*Brokers, error) {
	values := make([]namedservice.Entry[ConnectionName, Broker], len(entries))
	for i, e := range entries {
		values[i] = namedservice.Entry[ConnectionName, Broker]{Name: e.Name, Value: e.Value}
	}
	r, err := namedservice.New(selected, values)
	if err != nil {
		return nil, err
	}
	return &Brokers{registry: r}, nil
}
func (r *Brokers) Broker(name ConnectionName) (*Broker, error) {
	if r == nil {
		return (*namedservice.Registry[ConnectionName, Broker])(nil).Get(name)
	}
	return r.registry.Get(name)
}
func (r *Brokers) Default() (*Broker, error) {
	if r == nil {
		return (*namedservice.Registry[ConnectionName, Broker])(nil).Default()
	}
	return r.registry.Default()
}
func (r *Brokers) Names() []ConnectionName {
	if r == nil {
		return nil
	}
	return r.registry.Names()
}
func (r *Brokers) DefaultName() ConnectionName {
	if r == nil {
		return ""
	}
	return r.registry.DefaultName()
}
