package notifications

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	store "github.com/weiloon1234/Foundry-Go/internal/notificationstore"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/temporal"
	"github.com/weiloon1234/Foundry-Go/value"
)

// Record is a recipient-owned inbox result. Decode its private data using the
// declared DatabaseChannel, then construct a typed HTTP response DTO. Persistence
// rows and unrelated notification payloads never become dynamic response maps.
type Record[M any] struct {
	ID        ID[M]
	Name      Name
	Version   Version
	CreatedAt temporal.DateTime
	ReadAt    value.Nullable[temporal.DateTime]
	data      string
}

func (Record[M]) Format(s fmt.State, _ rune)   { _, _ = s.Write([]byte("notification inbox record")) }
func (Record[M]) MarshalJSON() ([]byte, error) { return nil, invalid() }

// Inbox obtains its recipient exclusively from the verified guard in ctx. It
// deliberately has no ListFor/ReadFor overload taking a client-supplied identity.
type Inbox[M model.Identifiable, K any] struct {
	manager   *Manager
	recipient Recipient[M, K]
}

func (r Recipient[M, K]) Inbox(manager *Manager) (Inbox[M, K], error) {
	if err := r.Validate(); err != nil {
		return Inbox[M, K]{}, err
	}
	if manager == nil || manager.registry == nil {
		return Inbox[M, K]{}, invalid()
	}
	for _, registration := range manager.registry.entries {
		if registration.recipientToken == r.token {
			return Inbox[M, K]{manager: manager, recipient: r}, nil
		}
	}
	return Inbox[M, K]{}, safeerror.NewFault(fault.Missing, "notification recipient is not registered")
}
func (i Inbox[M, K]) subject(ctx context.Context) (string, error) {
	if err := i.recipient.Validate(); err != nil {
		return "", err
	}
	origin, err := i.recipient.guard.Origin(ctx)
	if err != nil {
		return "", err
	}
	identity, present := origin.Model()
	if !present {
		return "", auth.Unauthenticated.WithCause(nil)
	}
	reference, err := i.recipient.provider.Parse(identity)
	if err != nil {
		return "", err
	}
	if _, err := i.recipient.provider.Resolve(ctx, reference); err != nil {
		return "", err
	}
	data, err := value.NewJSON(identity)
	if err != nil {
		return "", err
	}
	text, _ := data.Text()
	return digest(i.recipient.scope(), text), nil
}
func (i Inbox[M, K]) rows(subject string) orm.Query[store.Inbox] {
	f := store.Inboxes
	return store.Inboxes.Query().Where(orm.TextEq(f.Scope, i.recipient.scope()), orm.TextEq(f.SubjectKey, subject))
}
func inboxRecord[M any](row store.Inbox) (Record[M], error) {
	text, err := row.Data.Text()
	return Record[M]{ID: model.IDFromBytes[NotificationOf[M]](row.ID.Bytes()), Name: Name(row.Name), Version: Version(row.Version), CreatedAt: row.CreatedAt, ReadAt: row.ReadAt, data: text}, err
}

func (i Inbox[M, K]) List(ctx context.Context, page orm.PageRequest, unreadOnly bool) (orm.PageResult[Record[M]], error) {
	if err := page.Validate(); err != nil {
		return orm.PageResult[Record[M]]{}, err
	}
	ctx, release, err := i.manager.begin(ctx)
	if err != nil {
		return orm.PageResult[Record[M]]{}, err
	}
	defer release()
	subject, err := i.subject(ctx)
	if err != nil {
		return orm.PageResult[Record[M]]{}, err
	}
	var result orm.PageResult[Record[M]]
	err = i.manager.within(ctx, func(tx *database.Tx) error {
		q := i.rows(subject)
		if unreadOnly {
			q = q.Where(orm.NullableTimeIsNull(store.Inboxes.ReadAt))
		}
		page, err := orm.Paginate(ctx, tx, q.OrderBy(orm.TimeDesc(store.Inboxes.CreatedAt), orm.KeyDesc(store.Inboxes.ID)), page)
		if err != nil {
			return err
		}
		result = orm.PageResult[Record[M]]{Number: page.Number, Size: page.Size, Total: page.Total, Pages: page.Pages, Items: make([]Record[M], 0, len(page.Items))}
		for _, row := range page.Items {
			item, err := inboxRecord[M](row)
			if err != nil {
				return err
			}
			result.Items = append(result.Items, item)
		}
		return nil
	})
	if err != nil {
		return orm.PageResult[Record[M]]{}, err
	}
	return result, nil
}
func (i Inbox[M, K]) UnreadCount(ctx context.Context) (int64, error) {
	ctx, release, err := i.manager.begin(ctx)
	if err != nil {
		return 0, err
	}
	defer release()
	subject, err := i.subject(ctx)
	if err != nil {
		return 0, err
	}
	var count int64
	err = i.manager.within(ctx, func(tx *database.Tx) error {
		var err error
		count, err = orm.Count(ctx, tx, i.rows(subject).Where(orm.NullableTimeIsNull(store.Inboxes.ReadAt)))
		return err
	})
	return count, err
}

// MarkRead returns false for both an absent ID and another recipient's ID.
// Marking read repeatedly preserves the first read instant. MarkUnread clears it.
func (i Inbox[M, K]) MarkRead(ctx context.Context, id ID[M]) (bool, error) {
	return i.mark(ctx, id, true)
}
func (i Inbox[M, K]) MarkUnread(ctx context.Context, id ID[M]) (bool, error) {
	return i.mark(ctx, id, false)
}
func (i Inbox[M, K]) mark(ctx context.Context, id ID[M], read bool) (bool, error) {
	if id.IsZero() {
		return false, invalid()
	}
	ctx, release, err := i.manager.begin(ctx)
	if err != nil {
		return false, err
	}
	defer release()
	subject, err := i.subject(ctx)
	if err != nil {
		return false, err
	}
	found := false
	err = i.manager.within(ctx, func(tx *database.Tx) error {
		q := i.rows(subject)
		key := model.IDFromBytes[store.Inbox](id.Bytes())
		optional, err := orm.FirstLocked(ctx, tx, orm.Lock(q).Where(orm.KeyEq(store.Inboxes.Key(), key)))
		if err != nil {
			return err
		}
		row, present := optional.Get()
		if !present {
			return nil
		}
		found = true
		if read == !row.ReadAt.IsNull() {
			return nil
		}
		draft := []orm.Assignment[store.Inbox]{orm.NullableTimeSetNull(store.Inboxes.ReadAt)}
		if read {
			now, err := i.manager.now()
			if err != nil {
				return err
			}
			draft[0] = orm.NullableTimeSet(store.Inboxes.ReadAt, now)
		}
		_, err = orm.Update(ctx, tx, q, store.Inboxes.Key(), key, draft...)
		return err
	})
	return found && err == nil, err
}

// Status includes non-inbox channels and terminal ineligibility. Like inbox
// operations, it checks the guard, recipient scope and stored key before lookup.
func (i Inbox[M, K]) Status(ctx context.Context, id ID[M]) (Report[M], error) {
	ctx, release, err := i.manager.begin(ctx)
	if err != nil {
		return Report[M]{}, err
	}
	defer release()
	subject, err := i.subject(ctx)
	if err != nil {
		return Report[M]{}, err
	}
	key := model.IDFromBytes[store.Envelope](id.Bytes())
	err = i.manager.within(ctx, func(tx *database.Tx) error {
		f := store.Envelopes
		_, err := orm.Require(ctx, tx, store.Envelopes.Query().Where(orm.TextEq(f.Scope, i.recipient.scope()), orm.TextEq(f.SubjectKey, subject)).Where(orm.KeyEq(store.Envelopes.Key(), key)))
		return err
	})
	if err != nil {
		return Report[M]{}, err
	}
	_, _, deliveries, err := i.manager.load(ctx, key)
	if err != nil {
		return Report[M]{}, err
	}
	report := Report[M]{ID: id, Channels: make([]ChannelStatus, 0, len(deliveries))}
	for _, row := range deliveries {
		report.Channels = append(report.Channels, ChannelStatus{Channel: ChannelID(row.Channel), State: State(row.State), Attempts: row.Attempts})
	}
	return report, nil
}
