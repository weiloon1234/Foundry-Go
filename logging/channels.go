package logging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/dependency"
	"github.com/weiloon1234/Foundry-Go/internal/namedservice"
)

// Stack is valid only in ChannelSettings, not as an individual file/stream sink.
const Stack SinkDriver = "stack"

// ChannelSettings composes existing JSON sinks. Stack children are named channels;
// repeated leaves are written once per record, in declaration order.
//
//foundry:config
type ChannelSettings struct {
	Sink  SinkConfig
	Stack []ChannelName `config:",json"`
}

func DefaultChannelSettings() ChannelSettings { return ChannelSettings{Sink: DefaultSinkConfig()} }
func (s ChannelSettings) Validate() error {
	if s.Sink.Driver != Stack {
		if len(s.Stack) != 0 {
			return fault.New(fault.Invalid, "sink channel cannot have stack children")
		}
		return s.Sink.Validate()
	}
	if len(s.Stack) < 1 || len(s.Stack) > 16 || s.Sink.Path != "" || s.Sink.Level != 0 || s.Sink.AddSource || s.Sink.Rotation != (RotationConfig{}) || s.Sink.TimeZone != "" {
		return fault.New(fault.Invalid, "logging stack requires bounded children and leaf-owned sink options")
	}
	seen := make(map[ChannelName]bool, len(s.Stack))
	for _, name := range s.Stack {
		if err := name.Validate(); err != nil {
			return err
		}
		if seen[name] {
			return fault.New(fault.Duplicate, "logging stack repeats a child")
		}
		seen[name] = true
	}
	return nil
}

// ChannelSet owns configured sinks, never a borrowed default logger. Prepare is
// pure; Start acquires files and Close releases them after all borrowers drain.
// Copying the handle keeps one lifetime owner.
type ChannelSet struct{ state *channelSetState }
type channelSetState struct {
	mu              sync.Mutex
	channels        *Channels
	sinks           []*Sink
	started, closed bool
	closeErr        error
}

func PrepareChannels(selected ChannelName, settings map[ChannelName]ChannelSettings, borrowedDefault *slog.Logger) (*ChannelSet, error) {
	if err := selected.Validate(); err != nil {
		return nil, err
	}
	if len(settings) == 0 || len(settings) > namedservice.MaxEntries {
		return nil, fault.New(fault.Invalid, "logging requires bounded named channels")
	}
	if _, ok := settings[selected]; !ok {
		return nil, fault.New(fault.Missing, "default log channel is not configured")
	}
	settings = maps.Clone(settings)
	names := slices.Sorted(maps.Keys(settings))
	for _, name := range names {
		if err := name.Validate(); err != nil {
			return nil, err
		}
		item := settings[name]
		item.Stack = slices.Clone(item.Stack)
		settings[name] = item
		if err := item.Validate(); err != nil {
			return nil, err
		}
	}
	order, failure := dependency.Order(names, func(name ChannelName) ([]ChannelName, bool) { item, ok := settings[name]; return item.Stack, ok })
	if failure != nil {
		if failure.Kind == dependency.Cycle {
			return nil, fault.New(fault.Cycle, "logging stack contains a cycle")
		}
		return nil, fault.New(fault.Missing, "logging stack refers to an unknown channel")
	}
	state := &channelSetState{}
	values := make(map[ChannelName]*slog.Logger, len(settings))
	leaves := make(map[ChannelName][]ChannelName, len(settings))
	for _, name := range order {
		item := settings[name]
		if name == selected && borrowedDefault != nil {
			values[name] = borrowedDefault
			leaves[name] = []ChannelName{name}
			continue
		}
		if item.Sink.Driver != Stack {
			sink, err := PrepareSink(item.Sink)
			if err != nil {
				return nil, err
			}
			state.sinks = append(state.sinks, sink)
			values[name] = sink.Logger()
			leaves[name] = []ChannelName{name}
			continue
		}
		seen := make(map[ChannelName]bool)
		var handlers []slog.Handler
		for _, child := range item.Stack {
			for _, leaf := range leaves[child] {
				if seen[leaf] {
					continue
				}
				seen[leaf] = true
				leaves[name] = append(leaves[name], leaf)
				handlers = append(handlers, values[leaf].Handler())
			}
		}
		values[name] = slog.New(stackHandler{handlers})
	}
	entries := make([]NamedChannel, 0, len(names))
	for _, name := range names {
		entries = append(entries, NamedChannel{Name: name, Value: values[name]})
	}
	registry, err := NewChannels(selected, entries...)
	if err != nil {
		return nil, err
	}
	state.channels = registry
	return &ChannelSet{state}, nil
}
func (s *ChannelSet) Channels() *Channels {
	if s == nil || s.state == nil {
		return nil
	}
	return s.state.channels
}
func (s *ChannelSet) Start(ctx context.Context) error {
	if s == nil || s.state == nil || ctx == nil {
		return fault.New(fault.Invalid, "logging channels require an owner and context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	state := s.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.closed {
		return fault.New(fault.Closed, "logging channels are closed")
	}
	if state.started {
		return nil
	}
	for _, sink := range state.sinks {
		if err := sink.Start(ctx); err != nil {
			state.closed = true
			state.closeErr = closeSinks(state.sinks)
			return errors.Join(err, state.closeErr)
		}
	}
	state.started = true
	return nil
}
func (s *ChannelSet) Close() error {
	if s == nil || s.state == nil {
		return nil
	}
	state := s.state
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.closed {
		return state.closeErr
	}
	state.closed = true
	state.closeErr = closeSinks(state.sinks)
	return state.closeErr
}
func closeSinks(sinks []*Sink) error {
	var result error
	for i := len(sinks) - 1; i >= 0; i-- {
		result = errors.Join(result, sinks[i].Close())
	}
	return result
}
func (ChannelSet) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("logging channels")) }
func (ChannelSet) LogValue() slog.Value       { return slog.StringValue("logging channels") }

type stackHandler struct{ handlers []slog.Handler }

func (h stackHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, child := range h.handlers {
		if child.Enabled(ctx, level) {
			return true
		}
	}
	return false
}
func (h stackHandler) Handle(ctx context.Context, record slog.Record) error {
	var result error
	for _, child := range h.handlers {
		if child.Enabled(ctx, record.Level) {
			result = errors.Join(result, child.Handle(ctx, record.Clone()))
		}
	}
	return result
}
func (h stackHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	children := make([]slog.Handler, len(h.handlers))
	for i, child := range h.handlers {
		children[i] = child.WithAttrs(slices.Clone(attrs))
	}
	return stackHandler{children}
}
func (h stackHandler) WithGroup(name string) slog.Handler {
	children := make([]slog.Handler, len(h.handlers))
	for i, child := range h.handlers {
		children[i] = child.WithGroup(name)
	}
	return stackHandler{children}
}
