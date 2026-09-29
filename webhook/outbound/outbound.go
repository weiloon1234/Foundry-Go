// Package outbound delivers signed webhooks to registered endpoints. Events are
// typed by their generated JSON contracts, signed with Standard Webhooks
// (webhook-id, webhook-timestamp, webhook-signature) and sent through typed
// jobs from the transactional outbox with retries and a delivery log.
package outbound

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/httpclient"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/internal/identifier"
	"github.com/weiloon1234/Foundry-Go/internal/sqlname"
	"github.com/weiloon1234/Foundry-Go/internal/sqlscope"
	"github.com/weiloon1234/Foundry-Go/internal/workscope"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
)

// EventType names a delivered event, for example "invoice.paid".
type EventType string

func (t EventType) Validate() error {
	if !identifier.Semantic(string(t)) {
		return invalid()
	}
	return nil
}

// Event declares one outbound event and the generated JSON contract of its
// payload. Declare it once and reuse it for every Send and Publish.
type Event[P any] struct {
	kind     EventType
	contract contract.JSON[P]
}

func DefineEvent[P any](kind EventType, payload contract.JSON[P]) Event[P] {
	return Event[P]{kind: kind, contract: payload}
}
func (e Event[P]) Type() EventType { return e.kind }
func (e Event[P]) Validate() error {
	if err := e.kind.Validate(); err != nil {
		return err
	}
	return e.contract.Validate()
}
func (Event[P]) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("outbound webhook event")) }

// Endpoint and Delivery are identity markers for typed IDs.
type Endpoint struct{}
type Delivery struct{}
type EndpointID = model.ID[Endpoint]
type DeliveryID = model.ID[Delivery]

// Config bounds one outbound webhook service. MaxEndpoints bounds the registry
// and one Publish fan-out. MaxPayloadBytes bounds the signed body, including
// the envelope. SecretGrace is the default overlap during secret rotation, in
// which the previous secret still signs. Operations queue for capacity for at
// most min(Timeout, 5s) and then fail as retryable overload.
type Config struct {
	Schema          string
	MaxEndpoints    int
	MaxPayloadBytes int64
	MaxActive       int
	Timeout         time.Duration
	SecretGrace     time.Duration
}

const (
	MaxPayloadBytes      = 1 << 20
	MaxEndpointEvents    = 64
	MaxSigningSecrets    = 4
	MaxSecretGrace       = 7 * 24 * time.Hour
	maxEndpointURLBytes  = 2048
	defaultMaxEndpoints  = 1000
	secretPurpose        = encryption.Purpose("foundry.webhooks.secret.v1")
	webhookUserAgent     = "Foundry-Webhooks"
	failureCategoryBytes = 64
)

func DefaultConfig() Config {
	return Config{Schema: "public", MaxEndpoints: defaultMaxEndpoints, MaxPayloadBytes: 256 << 10, MaxActive: 32, Timeout: time.Minute, SecretGrace: 24 * time.Hour}
}
func (c Config) Validate() error {
	if !sqlname.Valid(c.Schema) || c.MaxEndpoints < 1 || c.MaxEndpoints > 100000 || c.MaxPayloadBytes < 1024 || c.MaxPayloadBytes > MaxPayloadBytes || c.MaxActive < 1 || c.MaxActive > 1024 || c.Timeout <= 0 || c.Timeout > 10*time.Minute || c.SecretGrace < 0 || c.SecretGrace > MaxSecretGrace {
		return invalid()
	}
	return nil
}

// Dependencies are borrowed and must outlive the service. Keys encrypts
// endpoint signing secrets at rest. Client must enforce a restricted
// destination policy, because endpoint URLs are usually customer input.
type Dependencies struct {
	DB     *database.DB
	Keys   *encryption.Keyring
	Client *httpclient.Client
	Clock  clock.Clock
}

// Service owns bounded registry and delivery operations. Construction performs
// no I/O; apply Migrations with the ordinary migration runner first.
type Service struct {
	db     *database.DB
	keys   *encryption.Keyring
	client *httpclient.Client
	clock  clock.Clock
	config Config
	calls  *workscope.Group
}

func New(dependencies Dependencies, config Config) (*Service, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if dependencies.DB == nil || credential.IsNil(dependencies.Clock) {
		return nil, invalid()
	}
	if err := dependencies.Keys.Validate(); err != nil {
		return nil, err
	}
	if !dependencies.Client.RestrictsDestinations() {
		return nil, fault.New(fault.Invalid, "outbound webhooks require an HTTP client with a restricted destination policy")
	}
	calls, err := workscope.New(config.MaxActive, config.Timeout)
	if err != nil {
		return nil, err
	}
	return &Service{db: dependencies.DB, keys: dependencies.Keys, client: dependencies.Client, clock: dependencies.Clock, config: config, calls: calls}, nil
}
func (s *Service) Validate() error {
	if s == nil || s.db == nil || s.calls == nil || s.client == nil {
		return invalid()
	}
	return nil
}
func (s *Service) Config() Config {
	if s == nil {
		return Config{}
	}
	return s.config
}

// Close stops admitting operations and waits for actual exit; Done reports it.
func (s *Service) Close(ctx context.Context) error {
	if err := s.Validate(); err != nil {
		return err
	}
	return s.calls.Close(ctx)
}
func (s *Service) Done() <-chan struct{} {
	if s == nil {
		var g *workscope.Group
		return g.Done()
	}
	return s.calls.Done()
}
func (*Service) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("outbound webhook service"))
}

func (s *Service) now() (temporal.DateTime, error) {
	now, err := temporal.NewDateTime(s.clock.Now().UTC().Truncate(time.Microsecond))
	if err != nil || now.IsZero() {
		return temporal.DateTime{}, invalid()
	}
	return now, nil
}

// transaction runs fn in an owned transaction scoped to the service schema.
func (s *Service) transaction(ctx context.Context, readOnly bool, fn func(context.Context, *database.Tx) error) error {
	options := database.TxOptions{Isolation: database.ReadCommitted}
	if readOnly {
		options = database.TxOptions{Isolation: database.RepeatableRead, ReadOnly: true}
	}
	return s.db.Transaction(ctx, func(tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL search_path TO "`+s.config.Schema+`", pg_temp`); err != nil {
			return err
		}
		return fn(ctx, tx)
	}, options)
}

// join scopes writes inside the caller's business transaction.
func (s *Service) join(ctx context.Context, tx *database.Tx, fn func(context.Context, *database.Tx) error) error {
	return sqlscope.InSchema(ctx, tx, s.db, s.config.Schema, func(child *database.Tx) error { return fn(ctx, child) })
}

func invalid() error {
	return fault.New(fault.Invalid, "invalid outbound webhook declaration, endpoint or delivery")
}

func validEvents(events []EventType) error {
	if len(events) > MaxEndpointEvents {
		return invalid()
	}
	seen := make(map[EventType]bool, len(events))
	for _, event := range events {
		if err := event.Validate(); err != nil || seen[event] {
			return invalid()
		}
		seen[event] = true
	}
	return nil
}
func eventStrings(events []EventType) []string {
	result := make([]string, len(events))
	for i, event := range events {
		result[i] = string(event)
	}
	slices.Sort(result)
	return result
}
