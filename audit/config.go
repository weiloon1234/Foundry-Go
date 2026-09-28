// Package audit records model history and typed domain events through the actual
// business transaction. Registration is explicit and never migrates storage.
package audit

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
)

// Area identifies an application audit namespace. It is attribution metadata,
// not an authorization boundary; applications authorize access to history.
type Area string

func (a Area) Validate() error {
	if !identifier.Semantic(string(a)) {
		return fault.New(fault.Invalid, "audit area requires a semantic identifier")
	}
	return nil
}

// Config is immutable after recorder construction. Zero RetentionDays disables
// configured pruning. Nothing runs retention automatically.
type Config struct {
	Area          Area
	RetentionDays uint16
}

func DefaultConfig() Config      { return Config{Area: "application"} }
func (c Config) Validate() error { return c.Area.Validate() }

// Recorder has no independent resource lifetime or database pool. Its methods
// use caller-owned executors/transactions and are safe for concurrent operations.
type Recorder struct{ config Config }

func New(config Config) (*Recorder, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Recorder{config: config}, nil
}
func (*Recorder) Format(state fmt.State, _ rune) { _, _ = state.Write([]byte("audit recorder")) }

type areaContextKey struct{}

// WithArea scopes subsequent audit writes/reads while retaining cancellation and
// attribution. It does not authenticate the caller or grant history access.
func WithArea(ctx context.Context, area Area) (context.Context, error) {
	if ctx == nil {
		return nil, fault.New(fault.Invalid, "audit area requires a context")
	}
	if err := area.Validate(); err != nil {
		return nil, err
	}
	return context.WithValue(ctx, areaContextKey{}, area), nil
}

func (r *Recorder) area(ctx context.Context) (Area, error) {
	if r == nil || ctx == nil {
		return "", fault.New(fault.Invalid, "audit requires a recorder and context")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	area, ok := ctx.Value(areaContextKey{}).(Area)
	if !ok {
		area = r.config.Area
	}
	return area, area.Validate()
}
