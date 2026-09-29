package notifications

import (
	"context"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
	store "github.com/weiloon1234/Foundry-Go/internal/notificationstore"
	"github.com/weiloon1234/Foundry-Go/model"
)

// DeliveryInfo is safe operator metadata for one channel delivery: no payload,
// route or rendered content.
type DeliveryInfo struct {
	ID           DeliveryID     `json:"-"`
	Key          string         `json:"id"`
	Notification NotificationID `json:"notification_id"`
	Channel      ChannelID      `json:"channel"`
	Kind         string         `json:"kind"`
	State        State          `json:"state"`
	Attempts     uint32         `json:"attempts"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// ParseDeliveryID restores an operator-supplied delivery identity. It grants no
// authority; operator tooling must authorize its callers.
func ParseDeliveryID(text string) (DeliveryID, error) {
	id := DeliveryID{key: text}
	return id, id.Validate()
}

// MaxDeliveryPage bounds one operator listing.
const MaxDeliveryPage = 100

// ErrDeliveryAnchorMoved reports that the after delivery no longer exists or
// left the listed state, so the page position is lost. Restart the listing
// from the beginning (a zero after) instead of silently repeating rows.
var ErrDeliveryAnchorMoved = fault.New(fault.Conflict, "delivery page anchor no longer exists in this state; restart the listing")

// Deliveries lists up to limit deliveries in state, oldest update first. Pass
// the last returned ID as after to continue; resolving rows between pages can
// move them, and a moved or deleted anchor returns ErrDeliveryAnchorMoved. Use
// it to find Running and Uncertain deliveries that need an operator decision.
func (m *Manager) Deliveries(ctx context.Context, state State, limit int, after DeliveryID) ([]DeliveryInfo, error) {
	if !state.valid() || limit < 1 || limit > MaxDeliveryPage || after.key != "" && after.Validate() != nil {
		return nil, invalid()
	}
	ctx, release, err := m.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	fields := store.DeliveryFields()
	var result []DeliveryInfo
	err = m.within(ctx, func(tx *database.Tx) error {
		result = nil
		predicates := []query.Predicate[store.Delivery]{fields.State.Eq(string(state))}
		if after.key != "" {
			anchor, err := store.QueryFoundryNotificationDeliveries().Find(ctx, tx, after.key)
			if err != nil {
				return err
			}
			row, ok := anchor.Get()
			if !ok || row.State != string(state) {
				return ErrDeliveryAnchorMoved
			}
			predicates = append(predicates, query.Or(fields.UpdatedAt.Gt(row.UpdatedAt), query.And(fields.UpdatedAt.Eq(row.UpdatedAt), fields.Key.Gt(row.Key))))
		}
		rows, err := store.QueryFoundryNotificationDeliveries().Where(predicates...).OrderBy(fields.UpdatedAt.Asc(), fields.Key.Asc()).Limit(limit).All(ctx, tx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			result = append(result, DeliveryInfo{ID: DeliveryID{row.Key}, Key: row.Key, Notification: model.IDFromBytes[Notification](row.NotificationID.Bytes()), Channel: ChannelID(row.Channel), Kind: row.Kind, State: State(row.State), Attempts: row.Attempts, UpdatedAt: row.UpdatedAt.UTC()})
		}
		return nil
	})
	return result, err
}

// Resolution is an operator's decision about a Running or Uncertain delivery
// after checking the provider. Delivered records confirmed acceptance; Rejected
// records confirmed non-acceptance without sending again; Resend returns the
// frozen output to Prepared so the next delivery attempt sends it again.
type Resolution string

const (
	ResolveDelivered Resolution = "delivered"
	ResolveRejected  Resolution = "rejected"
	ResolveResend    Resolution = "resend"
)

// ResolveDelivery applies a resolution only while the delivery is still in the
// expected Running or Uncertain state; changed=false means it moved meanwhile.
// It also invalidates the running claim, so a process that was still active
// cannot overwrite the decision. Resolve Running only after confirming its
// process is gone: a live call may still reach the provider. Resend risks a
// duplicate if the provider did accept; follow it with Deliver or a job.
func (m *Manager) ResolveDelivery(ctx context.Context, id DeliveryID, expected State, resolution Resolution) (bool, error) {
	if id.Validate() != nil || expected != Running && expected != Uncertain {
		return false, invalid()
	}
	target := map[Resolution]State{ResolveDelivered: Delivered, ResolveRejected: Rejected, ResolveResend: Prepared}[resolution]
	if target == "" {
		return false, invalid()
	}
	ctx, release, err := m.begin(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	changed := false
	err = m.within(ctx, func(tx *database.Tx) error {
		changed = false
		row, err := lockDelivery(ctx, tx, id.key)
		if err != nil {
			return err
		}
		if State(row.State) != expected {
			return nil
		}
		if target == Prepared && row.Kind == "database" {
			return fault.New(fault.Invalid, "database notification deliveries are never running or uncertain")
		}
		now, err := m.now()
		if err != nil {
			return err
		}
		_, err = store.QueryFoundryNotificationDeliveries().Update(ctx, tx, row.Key, store.DeliveryDraft{}.SetState(string(target)).SetClaim(model.ID[store.Delivery]{}).SetUpdatedAt(now))
		changed = err == nil
		return err
	})
	return changed, err
}

// Deliver attempts every retryable channel of one stored notification now, as
// its delivery job does, and reports the resulting channel states.
func (m *Manager) Deliver(ctx context.Context, id NotificationID) ([]ChannelStatus, error) {
	if id.IsZero() {
		return nil, invalid()
	}
	ctx, release, err := m.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	return m.deliver(ctx, model.IDFromBytes[store.Envelope](id.Bytes()))
}
