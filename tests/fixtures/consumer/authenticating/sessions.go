package authenticating

import (
	"context"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	sessionpg "github.com/weiloon1234/Foundry-Go/auth/session/postgres"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/model"
)

type UserSessions = session.Sessions[models.User, model.ID[models.User]]
type UserSession = session.Info[models.User, model.ID[models.User]]
type IssuedSession = session.Issued[models.User, model.ID[models.User]]

// WebSessions composes a borrowed pool and the existing typed model provider.
// Explicit sessionpg.Migrations() are applied through the ordinary migration
// runner during deployment, never by this constructor or on every request.
func WebSessions(db *database.DB, provider UserProvider, persistence sessionpg.Config, config session.Config) (*UserSessions, error) {
	backend, err := sessionpg.New(db, persistence)
	if err != nil {
		return nil, err
	}
	store, err := session.NewStore(backend, config)
	if err != nil {
		return nil, err
	}
	return session.New(store, "users.web", provider, "users.session")
}

// StartVerifiedSession is called only after trusted credential verification.
// A request ID or an arbitrary model is not a verified proof.
func StartVerifiedSession(ctx context.Context, sessions *UserSessions, proof auth.Proof[models.User, model.ID[models.User]]) (IssuedSession, error) {
	return sessions.Issue(ctx, proof, session.IssueOptions{})
}
func ListedSessions(ctx context.Context, sessions *UserSessions, user models.User) ([]UserSession, error) {
	return sessions.List(ctx, user.FoundryReference())
}
func RevokeListedSession(ctx context.Context, sessions *UserSessions, user models.User, id session.ID[models.User]) (bool, error) {
	return sessions.RevokeID(ctx, user.FoundryReference(), id)
}
func SessionSubject(info UserSession) model.Reference[models.User, model.ID[models.User]] {
	return info.Subject()
}
