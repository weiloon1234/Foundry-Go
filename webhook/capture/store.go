package capture

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/capturelog"
	"github.com/weiloon1234/Foundry-Go/internal/httpcapture"
	store "github.com/weiloon1234/Foundry-Go/internal/webhookcapturestore"
)

// Dependencies are borrowed; the Store never closes them.
type Dependencies struct {
	DB *database.DB
	// Schema holds foundry_webhook_captures; it must be a valid identifier.
	Schema string
	// Clock supplies retention cutoffs. Receive times come from the kernel.
	Clock  clock.Clock
	Logger *slog.Logger
}

// Store owns the capture table: the single writer that drains the recorder,
// retention, and the bounded reads behind Routes. Applications hold it to
// mount Routes; Module drives its lifecycle. Construct it with New.
type Store struct {
	// log is the engine behind an interface, so naming a Store never
	// instantiates the generic engine.
	log   capturelog.Engine
	clock clock.Clock
}

// New validates its inputs without I/O. A nil recorder gives a store that
// reads and prunes but never writes, as when capture is disabled at runtime.
//
// Example: capture.New(recorder, capture.Dependencies{DB: db, Schema: "public", Clock: clock.System{}, Logger: logger}, capture.DefaultConfig())
func New(recorder *httpcapture.Recorder[httpcapture.Snapshot], deps Dependencies, config Config) (*Store, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if deps.Clock == nil || deps.Logger == nil {
		return nil, invalid("webhook capture store requires a clock and logger")
	}
	if recorder != nil && recorder.BodyBytes() != config.BodyBytes {
		return nil, invalid("webhook capture recorder and store limits differ")
	}
	p := config.policy()
	log, err := capturelog.New(store.Table(), recorder, func(s httpcapture.Snapshot, dropped uint64) (store.Capture, error) {
		row, err := redact(s, p)
		row.DroppedBefore = capturelog.DroppedBefore(dropped)
		return row, err
	}, capturelog.Dependencies{DB: deps.DB, Schema: deps.Schema, Clock: deps.Clock, Logger: deps.Logger}, capturelog.Limits{Retention: config.Retention, MaxRows: config.MaxRows})
	if err != nil {
		return nil, err
	}
	return &Store{log: log, clock: deps.Clock}, nil
}

// Format writes a fixed label without exposing dependencies or captures.
func (*Store) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("webhook capture store")) }

// Run writes captures and prunes the table until ctx ends. Database trouble
// never stops it: an unavailable table degrades capture with one logged
// reason, a failed batch is retried twice and then row by row, and only the
// rows that still fail are counted as lost.
func (s *Store) Run(ctx context.Context) error {
	return s.log.Run(ctx, capturelog.Roles{Write: true, Retain: true})
}

// Close stops admission, writes what remains within a short drain budget and
// counts the rest as lost. It runs after the HTTP kernel has drained.
func (s *Store) Close(ctx context.Context) error { return s.log.Close(ctx) }

// Done closes once Close has finished.
func (s *Store) Done() <-chan struct{} { return s.log.Done() }
