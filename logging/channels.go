package logging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/dependency"
	"github.com/weiloon1234/Foundry-Go/internal/namedservice"
)

// Stack is valid only in ChannelSettings, not as an individual file/stream sink.
const Stack SinkDriver = "stack"

// Custom selects a typed slog.Handler supplied by Go code through WithHandler.
// Deployment settings choose only its minimum Level.
const Custom SinkDriver = "custom"

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
	if s.Sink.Driver == Custom {
		sink := s.Sink
		if len(s.Stack) != 0 || sink.Path != "" || sink.AddSource || sink.Rotation != (RotationConfig{}) || sink.TimeZone != "" || sink.Async != (AsyncConfig{}) || sink.Syslog != (SyslogConfig{}) {
			return fault.New(fault.Invalid, "custom log channel selects only its level")
		}
		if sink.Level < slog.LevelDebug || sink.Level > slog.LevelError {
			return fault.New(fault.Invalid, "invalid logging level")
		}
		return nil
	}
	if s.Sink.Driver != Stack {
		if len(s.Stack) != 0 {
			return fault.New(fault.Invalid, "sink channel cannot have stack children")
		}
		return s.Sink.Validate()
	}
	if len(s.Stack) < 1 || len(s.Stack) > 16 || s.Sink.Path != "" || s.Sink.Level != 0 || s.Sink.AddSource || s.Sink.Rotation != (RotationConfig{}) || s.Sink.TimeZone != "" || s.Sink.Async != (AsyncConfig{}) || s.Sink.Syslog != (SyslogConfig{}) {
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
	leaves          []channelLeaf
	started, closed bool
	closeErr        error
}

// channelLeaf records an owned sink or custom handler for Stats.
type channelLeaf struct {
	name   ChannelName
	sink   *Sink
	custom *sinkEvents
}

// ChannelStats reports one owned leaf channel. Stacks report through their
// leaves; a borrowed default logger belongs to its owner and is not reported.
type ChannelStats struct {
	Channel ChannelName `json:"channel"`
	Sink    SinkStats   `json:"sink"`
}

// ChannelOption supplies typed Go values for configured channels.
type ChannelOption func(*channelOptions) error
type channelOptions struct{ handlers map[ChannelName]slog.Handler }

// WithHandler binds a custom slog.Handler to a channel. A channel absent from
// settings is added with the custom driver; a configured channel must select
// the custom driver, whose Level is applied as a minimum before the handler.
// The handler keeps its own ownership and receives records unchanged; wrap it
// with Correlate to add correlation and context fields.
func WithHandler(name ChannelName, handler slog.Handler) ChannelOption {
	return func(o *channelOptions) error {
		if err := name.Validate(); err != nil {
			return err
		}
		if nilHandler(handler) {
			return fault.New(fault.Invalid, "custom log handler is nil")
		}
		if _, exists := o.handlers[name]; exists {
			return fault.New(fault.Duplicate, "custom log handler is already bound")
		}
		o.handlers[name] = handler
		return nil
	}
}

func nilHandler(handler slog.Handler) bool {
	if handler == nil {
		return true
	}
	value := reflect.ValueOf(handler)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	}
	return false
}

func PrepareChannels(selected ChannelName, settings map[ChannelName]ChannelSettings, borrowedDefault *slog.Logger, options ...ChannelOption) (*ChannelSet, error) {
	if err := selected.Validate(); err != nil {
		return nil, err
	}
	configured := channelOptions{handlers: make(map[ChannelName]slog.Handler)}
	for _, option := range options {
		if option == nil {
			return nil, fault.New(fault.Invalid, "nil logging channel option")
		}
		if err := option(&configured); err != nil {
			return nil, err
		}
	}
	settings = maps.Clone(settings)
	if settings == nil {
		settings = make(map[ChannelName]ChannelSettings)
	}
	for name := range configured.handlers {
		if item, exists := settings[name]; !exists {
			settings[name] = ChannelSettings{Sink: SinkConfig{Driver: Custom}}
		} else if item.Sink.Driver != Custom {
			return nil, fault.New(fault.Invalid, "custom log handler requires a custom channel")
		}
	}
	if len(settings) == 0 || len(settings) > namedservice.MaxEntries {
		return nil, fault.New(fault.Invalid, "logging requires bounded named channels")
	}
	if _, ok := settings[selected]; !ok {
		return nil, fault.New(fault.Missing, "default log channel is not configured")
	}
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
		if item.Sink.Driver == Custom {
			handler, ok := configured.handlers[name]
			if !ok {
				return nil, fault.New(fault.Missing, "custom log channel has no handler")
			}
			events := newSinkEvents(Custom, nil)
			state.leaves = append(state.leaves, channelLeaf{name: name, custom: events})
			values[name] = slog.New(customHandler{next: handler, level: item.Sink.Level, events: events})
			leaves[name] = []ChannelName{name}
			continue
		}
		if item.Sink.Driver != Stack {
			sink, err := PrepareSink(item.Sink)
			if err != nil {
				return nil, err
			}
			state.sinks = append(state.sinks, sink)
			state.leaves = append(state.leaves, channelLeaf{name: name, sink: sink})
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

// Stats reports owned leaf channels in name order. Counters remain readable
// after Close.
func (s *ChannelSet) Stats() []ChannelStats {
	if s == nil || s.state == nil {
		return nil
	}
	result := make([]ChannelStats, 0, len(s.state.leaves))
	for _, leaf := range s.state.leaves {
		stats := leaf.custom.snapshot()
		if leaf.sink != nil {
			stats = leaf.sink.Stats()
		}
		result = append(result, ChannelStats{Channel: leaf.name, Sink: stats})
	}
	slices.SortFunc(result, func(a, b ChannelStats) int { return strings.Compare(string(a.Channel), string(b.Channel)) })
	return result
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

// Handle always offers the record to every enabled child. A failing child is
// counted by its own leaf and never prevents delivery to the others.
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

// customHandler applies a channel's minimum level and counts delivery outcomes
// of an application-supplied handler. The handler receives records unchanged.
type customHandler struct {
	next   slog.Handler
	level  slog.Level
	events *sinkEvents
}

func (h customHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level && h.next.Enabled(ctx, level)
}
func (h customHandler) Handle(ctx context.Context, record slog.Record) error {
	if err := h.next.Handle(ctx, record); err != nil {
		h.events.failures.Add(1)
		h.events.dropped.Add(1)
		return err
	}
	h.events.records.Add(1)
	return nil
}
func (h customHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h.next = h.next.WithAttrs(slices.Clone(attrs))
	return h
}
func (h customHandler) WithGroup(name string) slog.Handler {
	h.next = h.next.WithGroup(name)
	return h
}
