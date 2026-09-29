package outbound

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/encryption"
	"github.com/weiloon1234/Foundry-Go/fault"
	store "github.com/weiloon1234/Foundry-Go/internal/webhookstore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// EndpointOptions registers one receiver. URL is checked against the service
// client's destination policy before it is stored and again for every send.
// Events are the event types Publish fans out to this endpoint; Send can
// target any active endpoint directly.
type EndpointOptions struct {
	URL    string
	Events []EventType
}

func (s *Service) validateEndpoint(options EndpointOptions) error {
	if len(options.URL) == 0 || len(options.URL) > maxEndpointURLBytes {
		return invalid()
	}
	if err := s.client.Post(options.URL).Validate(); err != nil {
		return err
	}
	return validEvents(options.Events)
}

// EndpointInfo describes a registered endpoint. It never contains secrets.
type EndpointInfo struct {
	ID        EndpointID
	URL       string
	Events    []EventType
	Active    bool
	CreatedAt temporal.DateTime
}

func (EndpointInfo) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("webhook endpoint")) }

func endpointInfo(row store.Endpoint) (EndpointInfo, error) {
	events, err := row.Events.Decode()
	if err != nil {
		return EndpointInfo{}, err
	}
	typed := make([]EventType, len(events))
	for i, event := range events {
		typed[i] = EventType(event)
	}
	return EndpointInfo{ID: model.IDFromBytes[Endpoint](row.ID.Bytes()), URL: row.URL, Events: typed, Active: row.Active, CreatedAt: row.CreatedAt}, nil
}
func endpointKey(id EndpointID) model.ID[store.Endpoint] {
	return model.IDFromBytes[store.Endpoint](id.Bytes())
}

// CreateEndpoint registers an active endpoint and returns its first signing
// secret ("whsec_" + base64). The secret is shown once: give it to the
// receiver for verification; only its encrypted form is stored.
func (s *Service) CreateEndpoint(ctx context.Context, options EndpointOptions) (EndpointID, secret.String, error) {
	if err := s.Validate(); err != nil {
		return EndpointID{}, secret.String{}, err
	}
	if err := s.validateEndpoint(options); err != nil {
		return EndpointID{}, secret.String{}, err
	}
	events, err := value.NewJSON(eventStrings(options.Events))
	if err != nil {
		return EndpointID{}, secret.String{}, err
	}
	id, err := model.NewID[store.Endpoint]()
	if err != nil {
		return EndpointID{}, secret.String{}, err
	}
	var signing secret.String
	err = s.calls.Run(ctx, "webhook endpoint registration", func(ctx context.Context) error {
		return s.transaction(ctx, false, func(ctx context.Context, tx *database.Tx) error {
			count, err := store.QueryFoundryWebhookEndpoints().Count(ctx, tx)
			if err != nil {
				return err
			}
			if count >= int64(s.config.MaxEndpoints) {
				return fault.New(fault.Conflict, "webhook endpoint registry is full")
			}
			now, err := s.now()
			if err != nil {
				return err
			}
			if _, err := store.QueryFoundryWebhookEndpoints().Create(ctx, tx, store.EndpointDraft{}.SetID(id).SetURL(options.URL).SetEvents(events).SetActive(true).SetCreatedAt(now).SetUpdatedAt(now)); err != nil {
				return err
			}
			signing, err = s.addSecret(ctx, tx, id, now)
			return err
		})
	})
	if err != nil {
		return EndpointID{}, secret.String{}, err
	}
	return model.IDFromBytes[Endpoint](id.Bytes()), signing, nil
}

// UpdateEndpoint replaces an endpoint's URL, subscriptions and active flag.
// An inactive endpoint receives no new deliveries; pending ones fail.
func (s *Service) UpdateEndpoint(ctx context.Context, id EndpointID, options EndpointOptions, active bool) (EndpointInfo, error) {
	if err := s.Validate(); err != nil {
		return EndpointInfo{}, err
	}
	if id.IsZero() {
		return EndpointInfo{}, invalid()
	}
	if err := s.validateEndpoint(options); err != nil {
		return EndpointInfo{}, err
	}
	events, err := value.NewJSON(eventStrings(options.Events))
	if err != nil {
		return EndpointInfo{}, err
	}
	var result EndpointInfo
	err = s.calls.Run(ctx, "webhook endpoint update", func(ctx context.Context) error {
		return s.transaction(ctx, false, func(ctx context.Context, tx *database.Tx) error {
			now, err := s.now()
			if err != nil {
				return err
			}
			row, err := store.QueryFoundryWebhookEndpoints().Update(ctx, tx, endpointKey(id), store.EndpointDraft{}.SetURL(options.URL).SetEvents(events).SetActive(active).SetUpdatedAt(now))
			if err != nil {
				return err
			}
			result, err = endpointInfo(row)
			return err
		})
	})
	return result, err
}

// Endpoint returns one registered endpoint.
func (s *Service) Endpoint(ctx context.Context, id EndpointID) (EndpointInfo, error) {
	if err := s.Validate(); err != nil {
		return EndpointInfo{}, err
	}
	var result EndpointInfo
	err := s.calls.Run(ctx, "webhook endpoint lookup", func(ctx context.Context) error {
		return s.transaction(ctx, true, func(ctx context.Context, tx *database.Tx) error {
			row, err := store.QueryFoundryWebhookEndpoints().RequireFind(ctx, tx, endpointKey(id))
			if err != nil {
				return err
			}
			result, err = endpointInfo(row)
			return err
		})
	})
	return result, err
}

// Endpoints lists registered endpoints in ID order after the given ID.
func (s *Service) Endpoints(ctx context.Context, after EndpointID, limit int) ([]EndpointInfo, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 1000 {
		return nil, invalid()
	}
	var result []EndpointInfo
	err := s.calls.Run(ctx, "webhook endpoint listing", func(ctx context.Context) error {
		return s.transaction(ctx, true, func(ctx context.Context, tx *database.Tx) error {
			f := store.EndpointFields()
			q := store.QueryFoundryWebhookEndpoints()
			if !after.IsZero() {
				ordered := query.OrderedField[store.Endpoint, model.ID[store.Endpoint]]{ScalarField: f.ID}
				q = q.Where(ordered.Gt(endpointKey(after)))
			}
			rows, err := q.OrderBy(f.ID.Asc()).Limit(limit).All(ctx, tx)
			if err != nil {
				return err
			}
			for _, row := range rows {
				info, err := endpointInfo(row)
				if err != nil {
					return err
				}
				result = append(result, info)
			}
			return nil
		})
	})
	return result, err
}

// RotateSecret adds a new current signing secret and returns it once. Previous
// secrets keep signing, alongside the new one, for Config.SecretGrace, so
// receivers can switch keys without rejecting deliveries.
func (s *Service) RotateSecret(ctx context.Context, id EndpointID) (secret.String, error) {
	if err := s.Validate(); err != nil {
		return secret.String{}, err
	}
	return s.RotateSecretWithGrace(ctx, id, s.config.SecretGrace)
}

// RotateSecretWithGrace is RotateSecret with an explicit overlap. A zero
// grace revokes previous secrets immediately. At most MaxSigningSecrets
// secrets sign at a time.
func (s *Service) RotateSecretWithGrace(ctx context.Context, id EndpointID, grace time.Duration) (secret.String, error) {
	if err := s.Validate(); err != nil {
		return secret.String{}, err
	}
	if id.IsZero() || grace < 0 || grace > MaxSecretGrace {
		return secret.String{}, invalid()
	}
	var signing secret.String
	err := s.calls.Run(ctx, "webhook secret rotation", func(ctx context.Context) error {
		return s.transaction(ctx, false, func(ctx context.Context, tx *database.Tx) error {
			if _, err := store.QueryFoundryWebhookEndpoints().ForUpdate().RequireFind(ctx, tx, endpointKey(id)); err != nil {
				return err
			}
			now, err := s.now()
			if err != nil {
				return err
			}
			expires, err := temporal.NewDateTime(now.UTC().Add(grace))
			if err != nil {
				return err
			}
			active, err := s.signingSecrets(ctx, tx, endpointKey(id), now)
			if err != nil {
				return err
			}
			if len(active) >= MaxSigningSecrets && grace > 0 {
				return fault.New(fault.Conflict, "webhook endpoint has too many signing secrets in rotation")
			}
			for _, row := range active {
				current, present := row.ExpiresAt.Get()
				if !present || current.UTC().After(expires.UTC()) {
					if _, err := store.QueryFoundryWebhookSecrets().Update(ctx, tx, row.ID, store.SecretDraft{}.SetExpiresAt(expires)); err != nil {
						return err
					}
				}
			}
			signing, err = s.addSecret(ctx, tx, endpointKey(id), now)
			return err
		})
	})
	return signing, err
}

// addSecret generates, encrypts and stores a new current secret.
func (s *Service) addSecret(ctx context.Context, tx *database.Tx, endpoint model.ID[store.Endpoint], now temporal.DateTime) (secret.String, error) {
	material := make([]byte, 32)
	if _, err := rand.Read(material); err != nil {
		return secret.String{}, err
	}
	signing := secret.New("whsec_" + base64.StdEncoding.EncodeToString(material))
	id, err := model.NewID[store.Secret]()
	if err != nil {
		return secret.String{}, err
	}
	binding, err := secretContext(endpoint, id)
	if err != nil {
		return secret.String{}, err
	}
	sealed, err := s.keys.Encrypt(ctx, binding, signing)
	if err != nil {
		return secret.String{}, err
	}
	_, err = store.QueryFoundryWebhookSecrets().Create(ctx, tx, store.SecretDraft{}.SetID(id).SetEndpointID(endpoint).SetCiphertext(sealed.Encoded()).ClearExpiresAt().SetCreatedAt(now))
	if err != nil {
		return secret.String{}, err
	}
	return signing, nil
}

// secretContext binds each envelope to its endpoint and secret record.
func secretContext(endpoint model.ID[store.Endpoint], id model.ID[store.Secret]) (encryption.Context, error) {
	return encryption.NewContext(secretPurpose, secret.New(endpoint.String()+":"+id.String()))
}

// signingSecrets returns unexpired secrets, newest first.
func (s *Service) signingSecrets(ctx context.Context, tx *database.Tx, endpoint model.ID[store.Endpoint], now temporal.DateTime) ([]store.Secret, error) {
	f := store.SecretFields()
	rows, err := store.QueryFoundryWebhookSecrets().Where(f.EndpointID.Eq(endpoint), query.Or(f.ExpiresAt.IsNull(), f.ExpiresAt.Gt(now))).OrderBy(f.CreatedAt.Desc(), f.ID.Desc()).Limit(MaxSigningSecrets+1).All(ctx, tx)
	if err != nil {
		return nil, err
	}
	if len(rows) > MaxSigningSecrets {
		return nil, fault.New(fault.Conflict, "webhook endpoint has too many signing secrets")
	}
	return rows, nil
}

// decryptSecrets opens the signing secrets and returns their HMAC keys.
func (s *Service) decryptSecrets(ctx context.Context, rows []store.Secret) ([][]byte, error) {
	keys := make([][]byte, 0, len(rows))
	for _, row := range rows {
		binding, err := secretContext(row.EndpointID, row.ID)
		if err != nil {
			return nil, err
		}
		sealed, err := encryption.ParseCiphertext(row.Ciphertext)
		if err != nil {
			return nil, err
		}
		plaintext, err := s.keys.Decrypt(ctx, binding, sealed)
		if err != nil {
			return nil, err
		}
		encoded, ok := strings.CutPrefix(plaintext.Reveal(), "whsec_")
		material, err := base64.StdEncoding.DecodeString(encoded)
		if !ok || err != nil || len(material) < 24 || len(material) > 64 {
			return nil, fault.New(fault.Invalid, "stored webhook signing secret is malformed")
		}
		keys = append(keys, material)
	}
	if len(keys) == 0 {
		return nil, fault.New(fault.Missing, "webhook endpoint has no signing secret")
	}
	return slices.Clip(keys), nil
}
