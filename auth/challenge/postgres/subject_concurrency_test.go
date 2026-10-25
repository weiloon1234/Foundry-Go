package postgres_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth/challenge"
	challengepg "github.com/weiloon1234/Foundry-Go/auth/challenge/postgres"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
	"github.com/weiloon1234/Foundry-Go/keyspace"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	"github.com/weiloon1234/Foundry-Go/testkit"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

type challengeMember struct{ ID int64 }

func TestChallengeConcurrentFirstSubjectCreation(t *testing.T) {
	db := pgtest.Open(t)
	schema := pgtest.Namespace(t, db)
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+schema+`", pg_temp`); err != nil {
			return err
		}
		for _, migration := range challengepg.Migrations() {
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
	backend, err := challengepg.New(db, challengepg.Config{Schema: schema, Clock: testkit.NewClock(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := challenge.Bind(secret.New("concurrency-fixture-binding"))
	if err != nil {
		t.Fatal(err)
	}
	for _, purpose := range []challenge.Kind{challenge.ResetPassword, challenge.VerifyEmail} {
		address := challenge.Address{Namespace: keyspace.Namespace{Application: "foundry-challenge-tests", Environment: "test"}, Provider: "members", Model: "challenge_members", Purpose: purpose}
		for id := int64(100); id < 104; id++ {
			identity, err := model.NewReference[challengeMember]("challenge_members", id, codec.Signed[int64]()).Identity()
			if err != nil {
				t.Fatal(err)
			}
			const workers = 8
			hashes := make([]challenge.Digest, workers)
			for i := range hashes {
				hashes[i], err = challenge.ParseDigest(fmt.Sprintf("%064x", id*workers+int64(i)+1))
				if err != nil {
					t.Fatal(err)
				}
			}
			start := make(chan struct{})
			failures := make(chan error, workers)
			var group sync.WaitGroup
			var lock sync.Mutex
			var prepared []challenge.Digest
			for _, hash := range hashes {
				group.Go(func() {
					<-start
					record, err := backend.Issue(t.Context(), address, identity, hash, time.Hour, func(context.Context, *database.Tx) (challenge.Binding, error) {
						lock.Lock()
						prepared = append(prepared, hash)
						lock.Unlock()
						return binding, nil
					})
					if err == nil && (record.Subject != identity || record.Address != address || !record.Hash.Equal(hash) || !record.Binding.Equal(binding)) {
						err = fmt.Errorf("issued challenge changed identity or binding")
					}
					failures <- err
				})
			}
			close(start)
			group.Wait()
			close(failures)
			for err := range failures {
				if err != nil {
					t.Fatalf("first-subject issue failed: %+v", errordiag.Describe(err))
				}
			}
			if len(prepared) != workers {
				t.Fatal("issuance callback was omitted or replayed", len(prepared))
			}
			// Subject locking makes the last prepared challenge the sole live one.
			key, err := address.SubjectKey(identity)
			if err != nil {
				t.Fatal(err)
			}
			var entries int64
			if err := database.ScanOne(t.Context(), db, `SELECT count(*) FROM "`+schema+`".foundry_challenges WHERE subject_key = $1`, []any{key}, &entries); err != nil || entries != 1 {
				t.Fatal("replacement was not atomic", entries, err)
			}
			applied := 0
			for _, hash := range hashes {
				found, err := backend.Consume(t.Context(), address, hash, func(_ context.Context, _ *database.Tx, record challenge.Record) error {
					applied++
					if record.Subject != identity || !record.Binding.Equal(binding) {
						return fmt.Errorf("consumption changed subject or binding")
					}
					return nil
				})
				if err != nil || found.IsSet() != hash.Equal(prepared[len(prepared)-1]) {
					t.Fatal("replaced challenge remained usable or current one was lost", err)
				}
			}
			if applied != 1 {
				t.Fatal("challenge consumption was not single-use", applied)
			}
			found, err := backend.Consume(t.Context(), address, prepared[len(prepared)-1], func(context.Context, *database.Tx, challenge.Record) error {
				t.Error("consumed challenge replayed")
				return nil
			})
			if err != nil || found.IsSet() {
				t.Fatal("consumed challenge remained live", err)
			}
		}
	}
	var subjects int64
	if err := database.ScanOne(t.Context(), db, `SELECT count(*) FROM "`+schema+`".foundry_challenge_subjects`, nil, &subjects); err != nil || subjects != 8 {
		t.Fatal("subject or purpose identity was lost", subjects, err)
	}
}
