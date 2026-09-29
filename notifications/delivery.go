package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/weiloon1234/Foundry-Go/attribution"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	store "github.com/weiloon1234/Foundry-Go/internal/notificationstore"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/value"
)

func (m *Manager) load(ctx context.Context, id model.ID[store.Envelope]) (store.Envelope, *registration, []store.Delivery, error) {
	var envelope store.Envelope
	var registration *registration
	var deliveries []store.Delivery
	err := m.within(ctx, func(tx *database.Tx) error {
		var err error
		envelope, err = store.QueryFoundryNotifications().RequireFind(ctx, tx, id)
		if err != nil {
			return err
		}
		registration = m.registry.entries[registrationKey{RecipientName(envelope.Recipient), Name(envelope.Name), Version(envelope.Version)}]
		if registration == nil || registration.scope != envelope.Scope {
			return invalid()
		}
		identity, err := envelope.Identity.Decode()
		if err != nil || identity.Validate() != nil {
			return invalid()
		}
		text, _ := envelope.Identity.Text()
		payload, err := envelope.Payload.Text()
		if err != nil {
			return err
		}
		deliveries, err = store.QueryFoundryNotificationDeliveries().Where(store.DeliveryFields().NotificationID.Eq(id)).Limit(MaxChannels+1).All(ctx, tx)
		if err != nil {
			return err
		}
		// The fingerprint covers the channels selected at capture, recorded by
		// the delivery rows, so adding or removing a channel from the binding
		// later does not make older notifications unreadable.
		if len(deliveries) == 0 || len(deliveries) > MaxChannels || digest(registration.scope, text) != envelope.SubjectKey || fingerprintWith(registration, text, payload, storedChannels(deliveries)) != envelope.Fingerprint {
			return invalid()
		}
		for _, delivery := range deliveries {
			if !State(delivery.State).valid() || delivery.Key != deliveryKey(id, ChannelID(delivery.Channel)) {
				return invalid()
			}
		}
		return nil
	})
	return envelope, registration, deliveries, err
}

// outcomes reads only the delivery rows for the final status report. The
// envelope was verified by load at the start of this pass and is immutable, so
// the report skips reloading and re-digesting it.
func (m *Manager) outcomes(ctx context.Context, id model.ID[store.Envelope]) ([]store.Delivery, error) {
	var deliveries []store.Delivery
	err := m.within(ctx, func(tx *database.Tx) error {
		var err error
		deliveries, err = store.QueryFoundryNotificationDeliveries().Where(store.DeliveryFields().NotificationID.Eq(id)).Limit(MaxChannels+1).All(ctx, tx)
		if err != nil {
			return err
		}
		if len(deliveries) == 0 || len(deliveries) > MaxChannels {
			return invalid()
		}
		for _, delivery := range deliveries {
			if !State(delivery.State).valid() || delivery.Key != deliveryKey(id, ChannelID(delivery.Channel)) {
				return invalid()
			}
		}
		return nil
	})
	return deliveries, err
}

func (m *Manager) deliver(ctx context.Context, id model.ID[store.Envelope]) ([]ChannelStatus, error) {
	envelope, registration, deliveries, err := m.load(ctx, id)
	if err != nil {
		return nil, err
	}
	identity, err := envelope.Identity.Decode()
	if err != nil {
		return nil, err
	}
	origin, err := envelope.Origin.Decode()
	if err != nil {
		return nil, err
	}
	ctx, err = attribution.WithContext(ctx, origin)
	if err != nil {
		return nil, err
	}

	input, err := envelope.Payload.Decode()
	if err != nil {
		return nil, err
	}
	var failures []error
	for index, channel := range registration.channels {
		if ctx.Err() != nil {
			failures = append(failures, ctx.Err())
			break
		}
		// A channel added to the binding after capture has no row and is not
		// part of this notification; a retired channel's row is left untouched.
		row, captured := currentRow(deliveries, channel)
		if !captured || !State(row.State).Retryable() {
			continue
		}
		if err := m.attempt(ctx, envelope, identity, input, registration, index, row); err != nil {
			failures = append(failures, err)
		}
	}
	// This bounded cleanup only reads outcomes. It never sends again after a
	// deadline, and it cannot erase an already recorded provider acceptance.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	deliveries, err = m.outcomes(cleanup, id)
	if err != nil {
		return nil, errors.Join(append(failures, err)...)
	}
	statuses := make([]ChannelStatus, 0, len(deliveries))
	bad := false
	for _, channel := range registration.channels {
		for _, row := range deliveries {
			if row.Channel != string(channel.id) || row.Kind != channel.kind {
				continue
			}
			state := State(row.State)
			statuses = append(statuses, ChannelStatus{Channel: channel.id, State: state, Attempts: row.Attempts})
			if state != Delivered && state != Skipped && state != Ineligible {
				bad = true
			}
		}
	}
	if bad {
		failures = append(failures, fault.New(fault.Internal, "notification has undelivered channels"))
	}
	return statuses, errors.Join(failures...)
}

func (m *Manager) attempt(ctx context.Context, envelope store.Envelope, identity model.Identity, input json.RawMessage, registration *registration, index int, row store.Delivery) error {
	channel := registration.channels[index]
	subject, terminal, err := registration.check(ctx, identity, channel.id)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if terminal != "" {
		return m.finishUnclaimed(ctx, row.Key, "", terminal)
	}
	if State(row.State) == Pending {
		var rendered []byte
		metadata := DeliveryContext{Notification: model.IDFromBytes[Notification](envelope.ID.Bytes()), Delivery: DeliveryID{row.Key}, Name: Name(envelope.Name), Version: Version(envelope.Version), Channel: channel.id}
		err := callback.Isolated("render notification channel", func() error {
			var err error
			rendered, err = registration.render(ctx, index, subject, metadata, input)
			return err
		})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil || len(rendered) > MaxPayloadBytes {
			return m.finishUnclaimed(ctx, row.Key, Pending, Rejected)
		}
		snapshot, err := value.ParseJSON[json.RawMessage](string(rendered))
		if err != nil {
			return m.finishUnclaimed(ctx, row.Key, Pending, Rejected)
		}
		err = m.within(ctx, func(tx *database.Tx) error {
			current, err := lockDelivery(ctx, tx, row.Key)
			if err != nil {
				return err
			}
			row = current
			if State(current.State) != Pending {
				return nil
			}
			now, err := m.now()
			if err != nil {
				return err
			}
			row, err = store.QueryFoundryNotificationDeliveries().Update(ctx, tx, row.Key, store.DeliveryDraft{}.SetPayload(snapshot).SetState(string(Prepared)).SetUpdatedAt(now))
			return err
		})
		if err != nil {
			return err
		}
		// no external work has started; check any revocation during rendering.
		if State(row.State) == Prepared {
			_, terminal, err := registration.check(ctx, identity, channel.id)
			if err != nil {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if terminal != "" {
				return m.finishUnclaimed(ctx, row.Key, "", terminal)
			}
		}
	}

	if State(row.State) != Prepared {
		return nil
	}
	data, err := row.Payload.Decode()
	if err != nil {
		return m.finishUnclaimed(ctx, row.Key, Prepared, Rejected)
	}
	if channel.kind == "database" {
		outcome := Reject
		err := callback.Isolated("validate notification inbox data", func() error {
			var err error
			outcome, err = channel.deliver(ctx, identity, DeliveryID{row.Key}, data)
			return err
		})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil || outcome != Accepted {
			return m.finishUnclaimed(ctx, row.Key, Prepared, Rejected)
		}
		return m.completeDatabase(ctx, envelope, row)
	}
	claim, err := model.NewID[store.Delivery]()
	if err != nil {
		return err
	}
	owned := false
	err = m.within(ctx, func(tx *database.Tx) error {
		current, err := lockDelivery(ctx, tx, row.Key)
		if err != nil {
			return err
		}
		if State(current.State) != Prepared {
			return nil
		}
		if current.Attempts == math.MaxUint32 {
			return invalid()
		}
		now, err := m.now()
		if err != nil {
			return err
		}
		row, err = store.QueryFoundryNotificationDeliveries().Update(ctx, tx, row.Key, store.DeliveryDraft{}.SetClaim(claim).SetState(string(Running)).SetAttempts(current.Attempts+1).SetUpdatedAt(now))
		owned = err == nil
		return err
	})
	if err != nil || !owned {
		return err
	}
	outcome := Retry // no transport was called when cancellation already arrived
	if ctx.Err() == nil {
		failure := callback.Isolated("deliver notification channel", func() error {
			var err error
			outcome, err = channel.deliver(ctx, identity, DeliveryID{row.Key}, data)
			return err
		})
		if failure != nil {
			outcome = Unknown
		}
	}
	state := Uncertain
	switch outcome {
	case Accepted:
		state = Delivered
	case Retry:
		state = Prepared
	case Reject:
		state = Rejected
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return m.within(cleanup, func(tx *database.Tx) error {
		current, err := lockDelivery(cleanup, tx, row.Key)
		if err != nil {
			return err
		}
		if State(current.State) != Running || current.Claim != claim {
			return fault.New(fault.Conflict, "notification delivery claim changed")
		}
		now, err := m.now()
		if err != nil {
			return err
		}
		_, err = store.QueryFoundryNotificationDeliveries().Update(cleanup, tx, row.Key, store.DeliveryDraft{}.SetState(string(state)).SetUpdatedAt(now))
		return err
	})
}

func lockDelivery(ctx context.Context, tx *database.Tx, key string) (store.Delivery, error) {
	return store.QueryFoundryNotificationDeliveries().ForUpdate().RequireFind(ctx, tx, key)
}
func (m *Manager) finishUnclaimed(ctx context.Context, key string, expected, state State) error {
	return m.within(ctx, func(tx *database.Tx) error {
		row, err := lockDelivery(ctx, tx, key)
		if err != nil || !State(row.State).Retryable() || expected != "" && State(row.State) != expected {
			return err
		}
		now, err := m.now()
		if err != nil {
			return err
		}
		_, err = store.QueryFoundryNotificationDeliveries().Update(ctx, tx, key, store.DeliveryDraft{}.SetState(string(state)).SetUpdatedAt(now))
		return err
	})
}
func (m *Manager) completeDatabase(ctx context.Context, envelope store.Envelope, prepared store.Delivery) error {
	return m.within(ctx, func(tx *database.Tx) error {
		row, err := lockDelivery(ctx, tx, prepared.Key)
		if err != nil || State(row.State) != Prepared {
			return err
		}
		if row.Attempts == math.MaxUint32 {
			return invalid()
		}
		now, err := m.now()
		if err != nil {
			return err
		}
		draft := store.InboxDraft{}.SetID(model.IDFromBytes[store.Inbox](envelope.ID.Bytes())).SetScope(envelope.Scope).SetSubjectKey(envelope.SubjectKey).
			SetName(envelope.Name).SetVersion(envelope.Version).SetData(row.Payload).SetCreatedAt(envelope.CreatedAt)
		if _, err := store.QueryFoundryNotificationInbox().Create(ctx, tx, draft); err != nil {
			return err
		}
		_, err = store.QueryFoundryNotificationDeliveries().Update(ctx, tx, row.Key, store.DeliveryDraft{}.SetState(string(Delivered)).SetAttempts(row.Attempts+1).SetUpdatedAt(now))
		return err
	})
}

// currentRow selects the stored row for a channel still bound with the same kind.
func currentRow(deliveries []store.Delivery, channel channelDefinition) (store.Delivery, bool) {
	for _, candidate := range deliveries {
		if candidate.Channel == string(channel.id) && candidate.Kind == channel.kind {
			return candidate, true
		}
	}
	return store.Delivery{}, false
}
