package recovering_test

import (
	"context"
	"errors"
	"testing"

	"foundry.test/consumer/recovering"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	sessionpg "github.com/weiloon1234/Foundry-Go/auth/session/postgres"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	tokenpg "github.com/weiloon1234/Foundry-Go/auth/token/postgres"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

type credentials struct {
	sessions    *recovering.Sessions
	tokens      *recovering.Tokens
	revocations *recovering.Revocations
	registry    *auth.Registry
}

func attachCredentials(t *testing.T, s *fixture) *credentials {
	t.Helper()
	err := s.within(t.Context(), func(tx *database.Tx) error {
		definitions := append(sessionpg.Migrations(), tokenpg.Migrations()...)
		for _, definition := range definitions {
			for _, sql := range definition.SQL {
				if _, err := tx.Exec(t.Context(), sql); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	webBackend, err := sessionpg.New(s.db, sessionpg.Config{Schema: s.schema, Clock: s.clock})
	if err != nil {
		t.Fatal(err)
	}
	webStore, err := session.NewStore(webBackend, session.DefaultConfig(s.namespace))
	if err != nil {
		t.Fatal(err)
	}
	web, err := session.New(webStore, "recovery.web", s.provider, "recovery.session")
	if err != nil {
		t.Fatal(err)
	}
	apiBackend, err := tokenpg.New(s.db, tokenpg.Config{Schema: s.schema, Clock: s.clock})
	if err != nil {
		t.Fatal(err)
	}
	apiStore, err := token.NewStore(apiBackend, token.DefaultConfig(s.namespace))
	if err != nil {
		t.Fatal(err)
	}
	api, err := token.New(apiStore, "recovery.api", s.provider, "recovery.bearer", auth.AccessScopes[recovering.Member]{})
	if err != nil {
		t.Fatal(err)
	}
	group, err := recovering.CredentialRevocations(s.provider, web, api)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := auth.NewRegistry(auth.DefaultConfig(), web.Guard().Registration(), api.Guard().Registration())
	if err != nil {
		t.Fatal(err)
	}
	s.invalidate = group.Invalidate
	return &credentials{sessions: web, tokens: api, revocations: group, registry: registry}
}
func passwordProof(t *testing.T, s *fixture, text string) auth.Proof[recovering.Member, model.ID[recovering.Member]] {
	t.Helper()
	var result auth.PasswordResult[recovering.Member, model.ID[recovering.Member]]
	err := s.within(t.Context(), func(tx *database.Tx) error {
		login, err := recovering.NewLogin(tx, s.provider, s.hasher)
		if err != nil {
			return err
		}
		result, err = login.Authenticate(t.Context(), s.member.Email, plain(t, text))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Proof().HasIssuanceCheck() {
		t.Fatal("password proof lost its issuance check")
	}
	return result.Proof()
}
func issueCredentials(t *testing.T, c *credentials, p auth.Proof[recovering.Member, model.ID[recovering.Member]]) (secret.String, token.Issued[recovering.Member, model.ID[recovering.Member]]) {
	t.Helper()
	web, err := c.sessions.Issue(t.Context(), p, session.IssueOptions{})
	if err != nil {
		t.Fatal(err)
	}
	api, err := c.tokens.Issue(t.Context(), p, token.IssueOptions[recovering.Member]{Name: "Recovery fixture", Refresh: true})
	if err != nil {
		t.Fatal(err)
	}
	return web.Secret(), api
}
func authorize(t *testing.T, c *credentials, guard auth.Guard[recovering.Member], source auth.CredentialName, raw secret.String) error {
	t.Helper()
	input, err := auth.NewCredentials(auth.Credential{Name: source, Secret: raw})
	if err != nil {
		return err
	}
	scope, err := c.registry.NewScope(t.Context(), input)
	if err != nil {
		return err
	}
	defer scope.Close()
	_, err = guard.Require(scope.Context())
	return err
}
func assertRevoked(t *testing.T, c *credentials, web secret.String, api token.Issued[recovering.Member, model.ID[recovering.Member]]) {
	t.Helper()
	if err := authorize(t, c, c.sessions.Guard(), "recovery.session", web); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("session survived reset", err)
	}
	if err := authorize(t, c, c.tokens.Guard(), "recovery.bearer", api.AccessSecret()); !errors.Is(err, auth.Unauthenticated) {
		t.Fatal("token survived reset", err)
	}
	refresh, present := api.RefreshSecret().Get()
	if !present {
		t.Fatal("fixture refresh missing")
	}
	if result, err := c.tokens.Refresh(t.Context(), refresh); !errors.Is(err, auth.Unauthenticated) || !result.AccessSecret().IsZero() {
		t.Fatal("refresh revived reset credential", err)
	}
}
func TestResetRevokesSessionsTokensAndRejectsPreviouslyVerifiedPassword(t *testing.T) {
	s := prepare(t)
	c := attachCredentials(t, s)
	oldProof := passwordProof(t, s, "old long password")
	web, api := issueCredentials(t, c, oldProof)
	if err := authorize(t, c, c.sessions.Guard(), "recovery.session", web); err != nil {
		t.Fatal(err)
	}
	if err := authorize(t, c, c.tokens.Guard(), "recovery.bearer", api.AccessSecret()); err != nil {
		t.Fatal(err)
	}
	link := issue(t, s)
	if _, err := s.reset.Complete(t.Context(), link.Token(), plain(t, "new long password")); err != nil {
		t.Fatal(err)
	}
	assertRevoked(t, c, web, api)
	if issued, err := c.sessions.Issue(t.Context(), oldProof, session.IssueOptions{}); !errors.Is(err, auth.Unauthenticated) || !issued.Secret().IsZero() {
		t.Fatal("stale password proof issued session", err)
	}
	if issued, err := c.tokens.Issue(t.Context(), oldProof, token.IssueOptions[recovering.Member]{}); !errors.Is(err, auth.Unauthenticated) || !issued.AccessSecret().IsZero() {
		t.Fatal("stale password proof issued token", err)
	}
	fresh := passwordProof(t, s, "new long password")
	web, api = issueCredentials(t, c, fresh)
	if err := authorize(t, c, c.sessions.Guard(), "recovery.session", web); err != nil {
		t.Fatal("new password cannot issue", err)
	}
	if err := authorize(t, c, c.tokens.Guard(), "recovery.bearer", api.AccessSecret()); err != nil {
		t.Fatal("new password cannot issue token", err)
	}
}
func TestResetRollbackRestoresAllGuardCredentialsAndLink(t *testing.T) {
	s := prepare(t)
	c := attachCredentials(t, s)
	web, api := issueCredentials(t, c, passwordProof(t, s, "old long password"))
	link := issue(t, s)
	cause := errors.New("later revocation contribution failed")
	webTarget, err := c.sessions.Revocation()
	if err != nil {
		t.Fatal(err)
	}
	apiTarget, err := c.tokens.Revocation()
	if err != nil {
		t.Fatal(err)
	}
	fail := auth.DefineRevocation("zz.failure", s.provider, func(context.Context, *database.Tx, model.Reference[recovering.Member, model.ID[recovering.Member]]) (uint64, error) {
		return 0, cause
	})
	group, err := auth.NewRevocations(s.provider, fail, apiTarget, webTarget)
	if err != nil {
		t.Fatal(err)
	}
	s.invalidate = group.Invalidate
	if _, err := s.reset.Complete(t.Context(), link.Token(), plain(t, "new long password")); !errors.Is(err, cause) {
		t.Fatal("reset ignored revocation failure", err)
	}
	if current(t, s).Password != s.member.Password {
		t.Fatal("failed revocation committed password")
	}
	if err := authorize(t, c, c.sessions.Guard(), "recovery.session", web); err != nil {
		t.Fatal("rollback lost session", err)
	}
	if err := authorize(t, c, c.tokens.Guard(), "recovery.bearer", api.AccessSecret()); err != nil {
		t.Fatal("rollback lost token", err)
	}
	s.invalidate = c.revocations.Invalidate
	if _, err := s.reset.Complete(t.Context(), link.Token(), plain(t, "new long password")); err != nil {
		t.Fatal("rollback consumed reset link", err)
	}
	assertRevoked(t, c, web, api)
}
func TestResetRacesCannotLeaveUsableCredentials(t *testing.T) {
	for _, race := range []string{"issue", "refresh"} {
		t.Run(race, func(t *testing.T) {
			s := prepare(t)
			c := attachCredentials(t, s)
			proof := passwordProof(t, s, "old long password")
			web, api := issueCredentials(t, c, proof)
			link := issue(t, s)
			type result struct {
				secret secret.String
				err    error
			}
			ready := make(chan struct{})
			done := make(chan result, 1)
			go func() {
				<-ready
				if race == "issue" {
					issued, err := c.sessions.Issue(t.Context(), proof, session.IssueOptions{})
					done <- result{secret: issued.Secret(), err: err}
					return
				}
				refresh, _ := api.RefreshSecret().Get()
				issued, err := c.tokens.Refresh(t.Context(), refresh)
				done <- result{secret: issued.AccessSecret(), err: err}
			}()
			close(ready)
			_, resetErr := s.reset.Complete(t.Context(), link.Token(), plain(t, "new long password"))
			concurrent := <-done
			if resetErr != nil {
				t.Fatal(resetErr)
			}
			if concurrent.err != nil && !errors.Is(concurrent.err, auth.Unauthenticated) {
				t.Fatal("race failed unexpectedly", concurrent.err)
			}
			if concurrent.err == nil {
				guard, source := c.sessions.Guard(), auth.CredentialName("recovery.session")
				if race == "refresh" {
					guard, source = c.tokens.Guard(), "recovery.bearer"
				}
				if err := authorize(t, c, guard, source, concurrent.secret); !errors.Is(err, auth.Unauthenticated) {
					t.Fatal("racing operation revived authority", err)
				}
			}
			assertRevoked(t, c, web, api)
			if rows, err := c.sessions.List(t.Context(), s.member.FoundryReference()); err != nil || len(rows) != 0 {
				t.Fatal("session rows survived race", err)
			}
			if rows, err := c.tokens.List(t.Context(), s.member.FoundryReference()); err != nil || len(rows) != 0 {
				t.Fatal("token rows survived race", err)
			}
		})
	}
}
func TestTransactionalRevocationRestoresSchemaAndRejectsForeignPool(t *testing.T) {
	s := prepare(t)
	c := attachCredentials(t, s)
	_, _ = issueCredentials(t, c, passwordProof(t, s, "old long password"))
	other := pgtest.Open(t)
	if err := other.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := c.sessions.RevokeAllIn(t.Context(), tx, s.member.FoundryReference()); !errors.Is(err, fault.Invalid) {
			return errors.New("foreign pool accepted")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.within(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO public, "`+s.schema+`"`); err != nil {
			return err
		}
		var before, after string
		if err := database.ScanOne(t.Context(), tx, `SELECT pg_catalog.current_setting('search_path')`, nil, &before); err != nil {
			return err
		}
		if count, err := c.sessions.RevokeAllIn(t.Context(), tx, s.member.FoundryReference()); err != nil || count != 1 {
			return errors.New("joined revocation failed")
		}
		if err := database.ScanOne(t.Context(), tx, `SELECT pg_catalog.current_setting('search_path')`, nil, &after); err != nil {
			return err
		}
		if before != after {
			return errors.New("credential adapter leaked search path")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
