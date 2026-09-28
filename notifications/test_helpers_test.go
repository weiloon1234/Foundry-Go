package notifications

import (
	"context"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/contract"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/outboxtest"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Member struct {
	ID               int64
	Enabled, Allowed bool
	Email            string
}

func (m Member) FoundryReference() model.Reference[Member, int64] {
	return model.NewReference[Member]("notification_members", m.ID, codec.Signed[int64]())
}
func (m Member) FoundryIdentity() (model.Identity, error) { return m.FoundryReference().Identity() }
func (Member) AccessID() string                           { panic("presentation getter is not an ownership check") }

type Input struct {
	Text string `json:"text"`
}
type InboxData struct {
	Text string `json:"text"`
}

func textSchema[T any]() contract.JSON[T] {
	typ := reflect.TypeFor[T]()
	id := contract.TypeID(typ.PkgPath() + "." + typ.Name())
	return contract.DefineJSON[T](contract.Schema{Root: id, Types: []contract.Type{
		{ID: id, Kind: contract.ObjectKind, Properties: []contract.Property{{Name: "text", Type: "string", Required: true}}},
		{ID: "string", Kind: contract.StringKind},
	}})
}

type authority struct {
	lookupFailure error
	mu            sync.Mutex
	members       map[int64]Member
	provider      auth.Provider[Member, int64]
	guard         auth.Guard[Member]
	registry      *auth.Registry
	recipient     Recipient[Member, int64]
}

func newAuthority(t *testing.T) *authority {
	t.Helper()
	a := &authority{members: map[int64]Member{1: {ID: 1, Enabled: true, Allowed: true, Email: "one@example.test"}, 2: {ID: 2, Enabled: true, Allowed: true, Email: "two@example.test"}}}
	a.provider = auth.DefineProvider("notification.members", Member{}.FoundryReference(), func(_ context.Context, key int64) (value.Optional[Member], error) {
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.lookupFailure != nil {
			return value.Optional[Member]{}, a.lookupFailure
		}
		member, exists := a.members[key]
		if !exists {
			return value.Optional[Member]{}, nil
		}
		return value.Set(member), nil
	}, func(_ context.Context, member Member) (bool, error) { return member.Enabled, nil })
	strategy := auth.DefineStrategy("bearer", func(_ context.Context, token secret.String) (value.Optional[auth.Proof[Member, int64]], error) {
		key, err := strconv.ParseInt(token.Reveal(), 10, 64)
		if err != nil {
			return value.Optional[auth.Proof[Member, int64]]{}, nil
		}
		proof, err := auth.NewProof(Member{ID: key}.FoundryReference(), auth.Authenticated)
		return value.Set(proof), err
	})
	a.guard = auth.DefineGuard("notification.web", a.provider, strategy)
	var err error
	a.registry, err = auth.NewRegistry(auth.DefaultConfig(), a.guard.Registration())
	if err != nil {
		t.Fatal(err)
	}
	a.recipient = DefineRecipient("members", a.provider, a.guard, func(_ context.Context, m Member, _ Name, _ ChannelID) (bool, error) { return m.Allowed, nil })
	return a
}
func (a *authority) set(member Member) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.members[member.ID] = member
}
func (a *authority) remove(key int64) { a.mu.Lock(); defer a.mu.Unlock(); delete(a.members, key) }
func (a *authority) context(t *testing.T, key int64) context.Context {
	t.Helper()
	credentials, err := auth.NewCredentials(auth.Credential{Name: "bearer", Secret: secret.New(strconv.FormatInt(key, 10))})
	if err != nil {
		t.Fatal(err)
	}
	scope, err := a.registry.NewScope(t.Context(), credentials)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = scope.Close() })
	return scope.Context()
}
func databaseChannel() DatabaseChannel[Member, Input, InboxData] {
	return Database("inbox", textSchema[InboxData](), func(_ context.Context, _ Member, _ DeliveryContext, p Input) (InboxData, error) {
		return InboxData{p.Text}, nil
	})
}
func fixture(t *testing.T, registrations ...Registration) (*Manager, outboxtest.Writer) {
	t.Helper()
	w := outboxtest.Open(t)
	if err := w.Transaction(t.Context(), func(tx *database.Tx) error {
		for _, migration := range Migrations() {
			for _, statement := range migration.SQL {
				if _, err := tx.Exec(t.Context(), statement); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	r, err := NewRegistry(registrations...)
	if err != nil {
		t.Fatal(err)
	}
	c := DefaultConfig()
	c.Schema = w.Schema
	m, err := New(w.DB, r, c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return m, w
}
func captureNotification(t *testing.T, b Binding[Member, int64, Input], key int64, text string) PendingNotification[Member, Input] {
	t.Helper()
	p, err := b.Capture(t.Context(), Member{ID: key}.FoundryReference(), Input{text}, ID[Member]{})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

type transportFunc[D any] func(context.Context, DeliveryID, D) (Outcome, error)

func (f transportFunc[D]) Deliver(ctx context.Context, id DeliveryID, data D) (Outcome, error) {
	return f(ctx, id, data)
}
