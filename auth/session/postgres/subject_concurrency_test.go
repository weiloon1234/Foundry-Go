package postgres_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/session"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
)

func TestSessionConcurrentFirstSubjectCreation(t *testing.T) {
	s := prepare(t)
	const workers = 8
	config := s.config
	config.MaxPerSubject, config.Limit = workers, auth.RejectNew
	store, err := session.NewStore(s.backend, config)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := session.New(store, "members.web", s.provider, "web.session")
	if err != nil {
		t.Fatal(err)
	}
	for id := int64(100); id < 104; id++ {
		proof, err := auth.NewProof(member{ID: id}.reference(), auth.Authenticated)
		if err != nil {
			t.Fatal(err)
		}
		type outcome struct {
			issued session.Issued[member, int64]
			err    error
		}
		results := make(chan outcome, workers)
		start := make(chan struct{})
		var group sync.WaitGroup
		for range workers {
			group.Go(func() {
				<-start
				issued, err := sessions.Issue(t.Context(), proof, session.IssueOptions{})
				results <- outcome{issued, err}
			})
		}
		close(start)
		group.Wait()
		close(results)
		for result := range results {
			if result.err != nil {
				t.Fatalf("first-subject issue failed: %+v", errordiag.Describe(result.err))
			}
			if model, err := authenticate(t, sessions, result.issued.Secret()); err != nil || model.ID != id {
				t.Fatal("issued session lost identity", err)
			}
		}
		list, err := sessions.List(t.Context(), member{ID: id}.reference())
		if err != nil || len(list) != workers {
			t.Fatal("concurrent sessions were lost", len(list), err)
		}
		if _, err := sessions.Issue(t.Context(), proof, session.IssueOptions{}); !errors.Is(err, auth.CredentialLimit) {
			t.Fatal("capacity was not enforced", err)
		}
		if removed, err := sessions.RevokeAll(t.Context(), member{ID: id}.reference()); err != nil || removed != workers {
			t.Fatal("revocation lost sessions", removed, err)
		}
	}
	var subjects int64
	if err := database.ScanOne(t.Context(), s.db, `SELECT count(*) FROM "`+s.schema+`".foundry_session_subjects`, nil, &subjects); err != nil || subjects != 4 {
		t.Fatal("first-subject creation duplicated identities", subjects, err)
	}
}
