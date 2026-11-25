package authenticating

import (
	"context"

	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	sessionpg "github.com/weiloon1234/Foundry-Go/auth/session/postgres"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
)

// doc-example:begin session-registry
func sessionRegistry(db *database.DB, namespace keyspace.Namespace, users auth.Provider[models.User, model.ID[models.User]]) (*session.Sessions[models.User, model.ID[models.User]], *auth.Registry, error) {
	backend, err := sessionpg.New(db, sessionpg.DefaultConfig())
	if err != nil {
		return nil, nil, err
	}
	store, err := session.NewStore(backend, session.DefaultConfig(namespace))
	if err != nil {
		return nil, nil, err
	}
	web, err := session.New(store, "users.web", users, "users.session")
	if err != nil {
		return nil, nil, err
	}
	guard := web.Guard()
	registry, err := auth.NewRegistry(auth.DefaultConfig(), guard.Registration())
	if err != nil {
		return nil, nil, err
	}
	return web, registry, nil
}

// doc-example:end session-registry

// doc-example:begin remember-session
func rememberSession(ctx context.Context, web *session.Sessions[models.User, model.ID[models.User]], verifiedProof auth.Proof[models.User, model.ID[models.User]]) (session.Issued[models.User, model.ID[models.User]], session.Info[models.User, model.ID[models.User]], error) {
	issued, err := web.Issue(ctx, verifiedProof, session.IssueOptions{Remember: true})
	if err != nil {
		return session.Issued[models.User, model.ID[models.User]]{}, session.Info[models.User, model.ID[models.User]]{}, err
	}
	info := issued.Info()
	// Publish issued.Secret() only through an approved transport boundary.
	return issued, info, nil
}

// doc-example:end remember-session

// doc-example:begin rotate-session
func rotateSession(ctx context.Context, web *session.Sessions[models.User, model.ID[models.User]], currentSecret secret.String) (session.Issued[models.User, model.ID[models.User]], error) {
	return web.Rotate(ctx, currentSecret)
}

// doc-example:end rotate-session
