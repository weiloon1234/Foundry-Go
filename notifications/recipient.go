package notifications

import (
	"context"
	"fmt"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/model"
)

type declarationToken struct{ marker byte }

// Recipient binds one model/key authority and inbox guard. Preferences run with
// a freshly loaded eligible model before EACH channel attempt, including retries.
// A false preference permanently skips that channel for this notification.
type Recipient[M model.Identifiable, K any] struct {
	token       *declarationToken
	name        RecipientName
	provider    auth.Provider[M, K]
	guard       auth.Guard[M]
	preferences func(context.Context, M, Name, ChannelID) (bool, error)
}

func DefineRecipient[M model.Identifiable, K any](name RecipientName, provider auth.Provider[M, K], guard auth.Guard[M], preferences func(context.Context, M, Name, ChannelID) (bool, error)) Recipient[M, K] {
	return Recipient[M, K]{token: &declarationToken{}, name: name, provider: provider, guard: guard, preferences: preferences}
}
func (r Recipient[M, K]) Name() RecipientName { return r.name }
func (r Recipient[M, K]) Validate() error {
	if r.token == nil || !semantic(string(r.name)) || r.preferences == nil {
		return invalid()
	}
	return r.provider.ValidateGuard(r.guard)
}
func (r Recipient[M, K]) scope() string {
	return digest(string(r.name), string(r.provider.Name()), string(r.guard.Name()), r.provider.ModelName())
}
func (Recipient[M, K]) Format(s fmt.State, _ rune) {
	_, _ = s.Write([]byte("notification recipient declaration"))
}
