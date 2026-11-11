package capture

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/foundation"
	"github.com/weiloon1234/Foundry-Go/internal/capturelog"
	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
	store "github.com/weiloon1234/Foundry-Go/internal/outboundcapturestore"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

// Dependencies are borrowed; the Store never closes them.
type Dependencies struct {
	DB *database.DB
	// Schema holds foundry_outbound_captures; it must be a valid identifier.
	Schema string
	// Clock supplies retention cutoffs. Attempt times come from the client.
	Clock  clock.Clock
	Logger *slog.Logger
}

// Store owns foundry_outbound_captures: the single writer that drains the
// recorder, retention, and the bounded reads behind Routes. Applications hold
// it to mount Routes; Module drives its lifecycle. Construct it with New.
type Store struct {
	// log is the engine behind an interface, so naming a Store never
	// instantiates the generic engine.
	log   capturelog.Engine
	clock clock.Clock
}

// New validates its inputs without I/O. A nil recorder gives a store that
// reads and prunes but never writes, as when capture is disabled.
//
// Example: capture.New(recorder, capture.Dependencies{DB: db, Schema: "public", Clock: clock.System{}, Logger: logger}, capture.DefaultConfig())
func New(recorder *httpcapture.Outbound, deps Dependencies, config Config) (*Store, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if deps.Clock == nil || deps.Logger == nil {
		return nil, invalid("outbound capture store requires a clock and logger")
	}
	var queue *httpcapture.Recorder[httpcapture.Exchange]
	if recorder != nil {
		if queue = recorder.Recorder(); queue.BodyBytes() != config.BodyBytes {
			return nil, invalid("outbound capture recorder and store limits differ")
		}
	}
	p := config.policy()
	log, err := capturelog.New(store.Table(), queue, func(e httpcapture.Exchange, dropped uint64) (store.Capture, error) {
		return redact(e, dropped, p)
	}, capturelog.Dependencies{DB: deps.DB, Schema: deps.Schema, Clock: deps.Clock, Logger: deps.Logger}, capturelog.Limits{Retention: config.Retention, MaxRows: config.MaxRows})
	if err != nil {
		return nil, err
	}
	return &Store{log: log, clock: deps.Clock}, nil
}

// Format writes a fixed label without exposing dependencies or captures.
func (*Store) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("outbound capture store")) }

// Run writes captures and prunes the table until ctx ends. Database trouble
// never stops it: an unavailable table degrades capture with one logged
// reason, a failed batch is retried twice and then row by row, and only the
// rows that still fail are counted as lost.
func (s *Store) Run(ctx context.Context) error {
	return s.log.Run(ctx, capturelog.Roles{Write: true, Retain: true})
}

// Close stops admission, writes what remains within a short drain budget and
// counts the rest as lost. It runs after every client has drained.
func (s *Store) Close(ctx context.Context) error { return s.log.Close(ctx) }

// Done closes once Close has finished.
func (s *Store) Done() <-chan struct{} { return s.log.Done() }

// Module registers the store factory and, at boot, its cleanup before any
// work starts. Every process writes, because every process constructs its
// clients; the table is checked on the first capture, so a process that makes
// no call never queries it. Retention runs beside every kernel except CLI, or
// after Start without a selected kernel. Requires must name only the borrowed
// database provider, so the store boots before and closes after every client.
//
// Example: capture.Module("app.outbound-captures", key, requires, construct)
func Module(name foundation.ProviderID, key foundation.Key[*Store], requires []foundation.ProviderID, construct func(foundation.Resolver) (*Store, error)) foundation.Module {
	return foundation.Module{Name: name, Requires: slices.Clone(requires), OnRegister: func(r *foundation.Registrar) error {
		if construct == nil {
			return invalid("outbound capture module requires a store factory")
		}
		return foundation.Factory(r, key, construct)
	}, OnBoot: func(_ context.Context, r *foundation.Runtime) error {
		s, err := foundation.Resolve(r.Services(), key)
		if err != nil {
			return err
		}
		if s == nil || s.log == nil {
			return invalid("outbound capture factory returned an uninitialized store")
		}
		if err := r.OnShutdown("outbound-capture", func(ctx context.Context) error {
			err := s.Close(ctx)
			<-s.Done()
			return err
		}); err != nil {
			return safeerror.Join("outbound capture cleanup registration", err, s.Close(context.Background()))
		}
		roles := capturelog.Roles{Write: true, Retain: len(r.SelectedKernels()) == 0}
		for _, kernel := range r.SelectedKernels() {
			roles.Retain = roles.Retain || kernel != foundation.CLI
		}
		return r.Go("outbound-capture", func(ctx context.Context) error { return s.log.Run(ctx, roles) })
	}}
}
