package notifications

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	store "github.com/weiloon1234/Foundry-Go/internal/notificationstore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

// PendingNotification freezes input and recipient identity, not routes or
// preferences. Keep it for deliberate transaction/send retries using the same ID.
// It is a runtime handle; queue through Enqueue instead of serializing it.
type PendingNotification[M, P any] struct{ capture *captured }
type captured struct {
	id                   model.ID[store.Envelope]
	registration         *registration
	identity             value.JSON[model.Identity]
	origin               value.JSON[attribution.Origin]
	input                value.JSON[json.RawMessage]
	subject, fingerprint string
}

func (p PendingNotification[M, P]) ID() ID[M] {
	if p.capture == nil {
		return ID[M]{}
	}
	return model.IDFromBytes[NotificationOf[M]](p.capture.id.Bytes())
}
func (PendingNotification[M, P]) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("pending notification"))
}
func (PendingNotification[M, P]) MarshalJSON() ([]byte, error) { return nil, invalid() }

// Capture performs no lookup or I/O. A zero ID creates one; an explicit ID is
// useful across caller retries. Reusing an ID with changed input fails on store.
func (b Binding[M, K, P]) Capture(ctx context.Context, recipient model.Reference[M, K], input P, id ID[M]) (PendingNotification[M, P], error) {
	if ctx == nil {
		return PendingNotification[M, P]{}, invalid()
	}
	if err := b.Validate(); err != nil {
		return PendingNotification[M, P]{}, err
	}
	if err := ctx.Err(); err != nil {
		return PendingNotification[M, P]{}, err
	}
	var result PendingNotification[M, P]
	err := callback.Isolated("capture notification", func() error {
		identity, err := recipient.Identity()
		if err != nil {
			return err
		}
		if b.parse == nil {
			return invalid()
		}
		if err := b.parse(identity); err != nil {
			return err
		}
		data, err := b.definition.payload.Encode(ctx, input, payloadLimits())
		if err != nil {
			return err
		}
		snapshot, err := value.ParseJSON[json.RawMessage](string(data))
		if err != nil {
			return err
		}
		storedIdentity, err := value.NewJSON(identity)
		if err != nil {
			return err
		}
		origin, err := value.NewJSON(attribution.FromContext(ctx))
		if err != nil {
			return err
		}
		identityJSON, _ := storedIdentity.Text()
		payloadJSON, _ := snapshot.Text()
		if id.IsZero() {
			id, err = NewID[M]()
			if err != nil {
				return err
			}
		}
		r := b.registration.definition

		result.capture = &captured{id: model.IDFromBytes[store.Envelope](id.Bytes()), registration: r, identity: storedIdentity, origin: origin, input: snapshot, subject: digest(r.scope, identityJSON), fingerprint: requestFingerprint(r, identityJSON, payloadJSON)}
		return ctx.Err()
	})
	if err != nil {
		if ctx.Err() != nil {
			return PendingNotification[M, P]{}, ctx.Err()
		}
		return PendingNotification[M, P]{}, fault.New(fault.Invalid, "notification capture failed")
	}
	return result, nil
}
func (p PendingNotification[M, P]) check(m *Manager) error {
	if p.capture == nil || m.registry.entries[p.capture.registration.key] != p.capture.registration {
		return invalid()
	}
	return nil
}

// Send durably records the notification before delivery. Reusing this pending
// handle attempts only retryable channels. Inspect Report even when err != nil.
func (p PendingNotification[M, P]) Send(ctx context.Context, manager *Manager) (Report[M], error) {
	report := Report[M]{ID: p.ID()}
	ctx, release, err := manager.begin(ctx)
	if err != nil {
		return report, err
	}
	defer release()
	if err := p.check(manager); err != nil {
		return report, err
	}
	err = manager.within(ctx, func(tx *database.Tx) error { _, err := manager.persist(ctx, tx, p.capture); return err })
	if err != nil {
		return report, err
	}
	report.Channels, err = manager.deliver(ctx, p.capture.id)
	return report, err
}
func (b Binding[M, K, P]) Send(ctx context.Context, manager *Manager, recipient model.Reference[M, K], input P) (Report[M], error) {
	pending, err := b.Capture(ctx, recipient, input, ID[M]{})
	if err != nil {
		return Report[M]{}, err
	}
	return pending.Send(ctx, manager)
}

func (m *Manager) persist(ctx context.Context, tx *database.Tx, captured *captured) (store.Envelope, error) {
	now, err := m.now()
	if err != nil {
		return store.Envelope{}, err
	}
	r := captured.registration
	draft := store.EnvelopeDraft{}.SetID(captured.id).SetRecipient(string(r.key.recipient)).SetScope(r.scope).SetSubjectKey(captured.subject).
		SetIdentity(captured.identity).SetOrigin(captured.origin).SetName(string(r.key.name)).SetVersion(uint32(r.key.version)).SetPayload(captured.input).SetFingerprint(captured.fingerprint).SetCreatedAt(now)
	if _, err := store.QueryFoundryNotifications().Upsert(ctx, tx, draft, query.OnConflict(store.EnvelopeFields().ID).DoNothing()); err != nil {
		return store.Envelope{}, err
	}
	row, err := store.QueryFoundryNotifications().ForUpdate().RequireFind(ctx, tx, captured.id)
	if err != nil {
		return store.Envelope{}, err
	}
	if row.Scope != r.scope || row.SubjectKey != captured.subject {
		return store.Envelope{}, fault.New(fault.Conflict, "notification identity already has different input")
	}
	if row.Fingerprint != captured.fingerprint {
		// A notification captured before its binding's channel set changed keeps
		// its original channels. Compare the input against the stored channel
		// list; a differing input is still a conflict.
		deliveries, err := store.QueryFoundryNotificationDeliveries().Where(store.DeliveryFields().NotificationID.Eq(captured.id)).Limit(MaxChannels+1).All(ctx, tx)
		if err != nil {
			return store.Envelope{}, err
		}
		identity, _ := captured.identity.Text()
		payload, _ := captured.input.Text()
		if len(deliveries) == 0 || fingerprintWith(r, identity, payload, storedChannels(deliveries)) != row.Fingerprint {
			return store.Envelope{}, fault.New(fault.Conflict, "notification identity already has different input")
		}
		return row, nil
	}
	empty, err := value.ParseJSON[json.RawMessage]("null")
	if err != nil {
		return store.Envelope{}, err
	}
	for _, channel := range r.channels {
		key := deliveryKey(captured.id, channel.id)
		draft := store.DeliveryDraft{}.SetKey(key).SetNotificationID(captured.id).SetChannel(string(channel.id)).SetKind(channel.kind).
			SetState(string(Pending)).SetPayload(empty).SetClaim(model.ID[store.Delivery]{}).SetAttempts(0).SetUpdatedAt(now)
		if _, err := store.QueryFoundryNotificationDeliveries().Upsert(ctx, tx, draft, query.OnConflict(store.DeliveryFields().Key).DoNothing()); err != nil {
			return store.Envelope{}, err
		}
	}
	return row, nil
}
func deliveryKey(id model.ID[store.Envelope], channel ChannelID) string {
	return digest(id.String(), string(channel))
}

// requestFingerprint is shared by capture and persisted-input restoration. It
// covers the channel selection current at capture time.
func requestFingerprint(r *registration, identity, payload string) string {
	channels := make([]string, 0, len(r.channels))
	for _, channel := range r.channels {
		channels = append(channels, string(channel.id)+":"+channel.kind)
	}
	return fingerprintWith(r, identity, payload, channels)
}

// fingerprintWith verifies a stored notification against the channel list it
// was captured with (its delivery rows), so later binding changes stay readable.
func fingerprintWith(r *registration, identity, payload string, channels []string) string {
	parts := []string{r.scope, identity, string(r.key.name), stringVersion(r.key.version), payload}
	channels = append([]string(nil), channels...)
	sort.Strings(channels)
	return digest(append(parts, channels...)...)
}
func storedChannels(deliveries []store.Delivery) []string {
	channels := make([]string, 0, len(deliveries))
	for _, delivery := range deliveries {
		channels = append(channels, delivery.Channel+":"+delivery.Kind)
	}
	return channels
}
