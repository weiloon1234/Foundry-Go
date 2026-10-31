package logging

import (
	"github.com/weiloon1234/Foundry-Go/internal/namedservice"
	"log/slog"
)

// ChannelName selects an instance, distinct from names of other service families.
type ChannelName string

// Validate validates the logging channel name.
func (n ChannelName) Validate() error { return namedservice.Validate(string(n)) }

// NamedChannel borrows one existing instance. Selection never creates another owner.
type NamedChannel struct {
	Name  ChannelName
	Value *slog.Logger
}

// Channels is immutable; default and named selection alias the same instance.
type Channels struct {
	registry *namedservice.Registry[ChannelName, slog.Logger]
}

// NewChannels constructs a named logger registry with an explicit default selection.
func NewChannels(selected ChannelName, entries ...NamedChannel) (*Channels, error) {
	values := make([]namedservice.Entry[ChannelName, slog.Logger], len(entries))
	for i, e := range entries {
		values[i] = namedservice.Entry[ChannelName, slog.Logger]{Name: e.Name, Value: e.Value}
	}
	r, err := namedservice.New(selected, values)
	if err != nil {
		return nil, err
	}
	return &Channels{registry: r}, nil
}

// Channel looks up a named logger through the registry.
func (r *Channels) Channel(name ChannelName) (*slog.Logger, error) {
	if r == nil {
		return (*namedservice.Registry[ChannelName, slog.Logger])(nil).Get(name)
	}
	return r.registry.Get(name)
}

// Default returns the selected default logger or a registry error.
func (r *Channels) Default() (*slog.Logger, error) {
	if r == nil {
		return (*namedservice.Registry[ChannelName, slog.Logger])(nil).Default()
	}
	return r.registry.Default()
}

// Names returns channel names, or nil for a nil registry.
func (r *Channels) Names() []ChannelName {
	if r == nil {
		return nil
	}
	return r.registry.Names()
}

// DefaultName returns the selected default name, or an empty name for a nil registry.
func (r *Channels) DefaultName() ChannelName {
	if r == nil {
		return ""
	}
	return r.registry.DefaultName()
}
