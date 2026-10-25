package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/auth/token"
	"github.com/weiloon1234/Foundry-Go/database"
	dbpg "github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/credential"
	"github.com/weiloon1234/Foundry-Go/internal/errordiag"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

func TestTokenContentionClassification(t *testing.T) {
	for _, resource := range []string{"subject-lock", "pool"} {
		t.Run(resource, func(t *testing.T) {
			s := prepare(t, func(c *dbpg.Config) {
				if resource == "subject-lock" {
					c.LockTimeout = 50 * time.Millisecond
				} else {
					c.Pool.MaxOpen, c.Pool.MaxIdle = 1, 1
					c.Pool.AcquireTimeout = 50 * time.Millisecond
				}
			})
			first := issue(t, s, 7, true)
			proof, err := auth.NewProof(member{ID: 7}.reference(), auth.Authenticated)
			if err != nil {
				t.Fatal(err)
			}
			// Hold the lock (or the sole connection) until both operations return.
			// Neither contention case depends on scheduling or sleeps.
			within(t, s, func(tx *database.Tx) error {
				if _, err := tx.Exec(t.Context(), `SELECT key FROM foundry_token_subjects FOR UPDATE`); err != nil {
					return err
				}
				issued, err := s.tokens.Issue(t.Context(), proof, token.IssueOptions[member]{Scopes: s.grants, Refresh: true})
				assertContention(t, resource, err)
				if !issued.AccessSecret().IsZero() || issued.RefreshSecret().IsSet() || !issued.Info().ID().IsZero() {
					t.Error("failed issue published credentials")
				}
				removed, err := s.tokens.RevokeAll(t.Context(), member{ID: 7}.reference())
				assertContention(t, resource, err)
				if removed != 0 {
					t.Error("failed revocation published a count")
				}
				return nil
			})
			list, err := s.tokens.List(t.Context(), member{ID: 7}.reference())
			if err != nil || len(list) != 1 || list[0].ID() != first.Info().ID() {
				t.Fatal("contention changed persisted families", err)
			}
			if err := authenticate(t, s, first.AccessSecret()); err != nil {
				t.Fatal(err)
			}
			// A deliberate new operation works after releasing the held resource.
			_ = issue(t, s, 7, true)
			if removed, err := s.tokens.RevokeAll(t.Context(), member{ID: 7}.reference()); err != nil || removed != 2 {
				t.Fatal("resource was not released", removed, err)
			}
		})
	}
}

func assertContention(t *testing.T, resource string, err error) {
	t.Helper()
	code := fault.Conflict
	if resource == "pool" {
		code = fault.Overloaded
	}
	var classified *fault.Error
	if !errors.As(err, &classified) || classified.Code() != code {
		t.Errorf("contention was not classified as %s: %+v", code, errordiag.Describe(err))
	}
	var detail *database.Error
	if !errors.As(err, &detail) {
		t.Fatalf("database cause lost: %+v", errordiag.Describe(err))
	}
	if resource == "subject-lock" {
		if detail.Code() != database.LockNotAvailable || detail.SQLState() != "55P03" || detail.Outcome() != database.RolledBack {
			t.Errorf("lock failure lost classification/outcome: %+v", errordiag.Describe(err))
		}
	} else if !errors.Is(err, database.DeadlineExceeded) || !errors.Is(err, fault.Overloaded) {
		t.Errorf("pool admission classification lost: %+v", errordiag.Describe(err))
	}
	if errors.Is(err, database.CommitUnknown) {
		t.Error("known contention became an unknown commit")
	}
}

// Inject server SQLSTATEs to cover classification without scheduler-dependent
// deadlock-victim selection. The lock-timeout test above uses real contention.
func TestCredentialGateDatabaseOutcomes(t *testing.T) {
	s := prepare(t)
	var rollback error
	for _, test := range []struct {
		state string
		code  database.Code
	}{
		{"40001", database.SerializationFailure}, {"40P01", database.Deadlock},
	} {
		t.Run(test.state, func(t *testing.T) {
			calls := 0
			err := credential.NewGate(1, 30*time.Second).Execute(t.Context(), func(ctx context.Context) error {
				calls++
				rollback = s.db.Transaction(ctx, func(tx *database.Tx) error {
					_, err := tx.Exec(ctx, `DO $$ BEGIN RAISE EXCEPTION 'private-contention-sentinel' USING ERRCODE = '`+test.state+`'; END $$`)
					return err
				})
				return rollback
			})
			var detail *database.Error
			if calls != 1 || !errors.Is(err, fault.Conflict) || errors.Is(err, fault.Internal) || !errors.Is(err, test.code) || !errors.As(err, &detail) || detail.Outcome() != database.RolledBack {
				t.Fatalf("rollback classification or attempt count changed: calls=%d %+v", calls, errordiag.Describe(err))
			}
		})
	}
	committed := s.db.Transaction(t.Context(), func(tx *database.Tx) error {
		return tx.AfterCommit(func(context.Context) error { return errors.New("private-after-commit-sentinel") })
	})
	var committedDetail *database.Error
	if !errors.As(committed, &committedDetail) || committedDetail.Outcome() != database.Committed {
		t.Fatalf("missing committed test outcome: %+v", errordiag.Describe(committed))
	}
	unknown := database.FoundryStatementUnknown(sqlowner.Outcomes(), "test commit", errors.New("private-outcome-sentinel"))
	var unknownDetail *database.Error
	if !errors.As(unknown, &unknownDetail) || unknownDetail.Outcome() != database.Unknown {
		t.Fatal("missing unknown test outcome")
	}
	for _, detail := range []*database.Error{committedDetail, unknownDetail} {
		opaque := asDatabaseFailure{detail}
		for _, competing := range []error{fault.New(fault.Overloaded, "capacity exhausted"), rollback} {
			for _, cause := range []error{errors.Join(opaque, competing), errors.Join(competing, opaque)} {
				calls := 0
				err := credential.NewGate(1, 30*time.Second).Execute(t.Context(), func(context.Context) error { calls++; return cause })
				var classified *fault.Error
				if calls != 1 || !errors.As(err, &classified) || classified.Code() != fault.Internal {
					t.Fatal("As-only outcome lost precedence", calls, err)
				}
				var retained *database.Error
				if !errors.As(opaque, &retained) || retained != detail {
					t.Fatal("test did not expose outcome through As")
				}
			}
		}
	}
	for _, outcome := range []error{database.CommitUnknown, committed} {
		for _, cause := range []error{errors.Join(rollback, outcome), errors.Join(outcome, rollback)} {
			ctx, cancel := context.WithCancel(t.Context())
			calls := 0
			err := credential.NewGate(1, 30*time.Second).Execute(ctx, func(context.Context) error {
				calls++
				cancel() // Late cancellation must not erase the outcome either.
				return cause
			})
			cancel()
			var classified *fault.Error
			if calls != 1 || !errors.As(err, &classified) || classified.Code() != fault.Internal || !errors.Is(err, outcome) || !errors.Is(err, context.Canceled) {
				t.Fatalf("outcome was lost or replayed: calls=%d %+v", calls, errordiag.Describe(err))
			}
		}
	}
}

// No Unwrap or Is: the backend exposes this database outcome only through As.
type asDatabaseFailure struct{ detail *database.Error }

func (asDatabaseFailure) Error() string { return "private-outcome-sentinel" }
func (e asDatabaseFailure) As(target any) bool {
	if slot, ok := target.(**database.Error); ok {
		*slot = e.detail
		return true
	}
	return false
}
