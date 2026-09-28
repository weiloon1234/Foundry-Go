package notifications

import (
	"context"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/value"
)

type Administrator struct{ ID int64 }

func (m Administrator) FoundryReference() model.Reference[Administrator, int64] {
	return model.NewReference[Administrator]("notification_admins", m.ID, codec.Signed[int64]())
}
func (m Administrator) FoundryIdentity() (model.Identity, error) {
	return m.FoundryReference().Identity()
}

func TestNotificationStoredKeysCannotCrossModelOrGuardScopes(t *testing.T) {
	a := newAuthority(t)
	provider := auth.DefineProvider("admins", Administrator{}.FoundryReference(), func(_ context.Context, key int64) (value.Optional[Administrator], error) {
		return value.Set(Administrator{key}), nil
	}, func(context.Context, Administrator) (bool, error) { return true, nil })
	proof, err := auth.NewProof(Administrator{1}.FoundryReference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	strategy := auth.DefineStrategy("admin", func(context.Context, secret.String) (value.Optional[auth.Proof[Administrator, int64]], error) {
		return value.Set(proof), nil
	})
	guard := auth.DefineGuard("admins.web", provider, strategy)
	admins := DefineRecipient("admins", provider, guard, func(context.Context, Administrator, Name, ChannelID) (bool, error) { return true, nil })
	data := Database("inbox", textSchema[InboxData](), func(_ context.Context, _ Administrator, _ DeliveryContext, p Input) (InboxData, error) {
		return InboxData{p.Text}, nil
	})
	d := Define("notice", 1, textSchema[Input]())
	members := Bind(d, a.recipient, databaseChannel().Channel())
	adminBinding := Bind(d, admins, data.Channel())
	m, _ := fixture(t, members.Registration(), adminBinding.Registration())
	member := captureNotification(t, members, 1, "member-private")
	if _, err := member.Send(t.Context(), m); err != nil {
		t.Fatal(err)
	}
	admin, err := adminBinding.Capture(t.Context(), Administrator{1}.FoundryReference(), Input{"admin-private"}, ID[Administrator]{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Send(t.Context(), m); err != nil {
		t.Fatal(err)
	}
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration())
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := auth.NewCredentials(auth.Credential{Name: "admin", Secret: secret.New("fixture")})
	if err != nil {
		t.Fatal(err)
	}
	scope, err := registry.NewScope(t.Context(), credentials)
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()
	inbox, err := admins.Inbox(m)
	if err != nil {
		t.Fatal(err)
	}
	page, err := inbox.List(scope.Context(), query.PageRequest{Number: 1, Size: 10}, false)
	if err != nil || page.Total != 1 {
		t.Fatal("cross-model key collision", err)
	}
	decoded, err := data.Decode(scope.Context(), d, page.Items[0])
	if err != nil || decoded.Text != "admin-private" {
		t.Fatal("foreign data decoded", err)
	}
	// Reapplying a generic owner at the explicit transport boundary still grants
	// no authority: the stored scope/key predicate rejects the foreign row.
	forged := model.IDFromBytes[NotificationOf[Administrator]](member.ID().Bytes())
	if changed, err := inbox.MarkRead(scope.Context(), forged); err != nil || changed {
		t.Fatal("foreign row acknowledged", err)
	}
}
