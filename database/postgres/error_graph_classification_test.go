package postgres

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/weiloon1234/Foundry-Go/database"
)

type classificationCycle struct{ visits int }

func (*classificationCycle) Error() string   { panic("private classifier error") }
func (e *classificationCycle) Unwrap() error { e.visits++; return e }

type retryableClassificationError struct{}

func (retryableClassificationError) Error() string     { panic("private retry error") }
func (retryableClassificationError) SafeToRetry() bool { return true }

type timeoutClassificationError struct{}

func (timeoutClassificationError) Error() string   { panic("private timeout error") }
func (timeoutClassificationError) Timeout() bool   { return true }
func (timeoutClassificationError) Temporary() bool { return true }

func TestPostgresClassificationBoundsApplicationErrors(t *testing.T) {
	cycle := &classificationCycle{}
	got := classify(cycle)
	if got.Code != database.QueryFailed || got.StatementRejected || cycle.visits == 0 || cycle.visits > 256 {
		t.Fatal("cyclic classification lost bounds")
	}
	if got := classify(retryableClassificationError{}); got.Code != database.QueryFailed || !got.StatementRejected {
		t.Fatal("retry guarantee lost")
	}
	if got := classify(timeoutClassificationError{}); got.Code != database.DeadlineExceeded {
		t.Fatal("timeout classification lost")
	}
	got = classify(errors.Join(timeoutClassificationError{}, &pgconn.PgError{Code: "23505"}))
	if got.Code != database.UniqueViolation || !got.StatementRejected {
		t.Fatal("server error precedence changed")
	}
}

type retryTrait struct{}

func (retryTrait) SafeToRetry() bool { return true }

type retryTraitProxy struct{}

func (retryTraitProxy) Error() string { panic("private retry proxy") }
func (retryTraitProxy) As(target any) bool {
	if trait, ok := target.(*interface{ SafeToRetry() bool }); ok {
		*trait = retryTrait{}
		return true
	}
	return false
}
func TestPostgresClassificationPreservesRetryTraitAsTarget(t *testing.T) {
	if got := classify(retryTraitProxy{}); got.Code != database.QueryFailed || !got.StatementRejected {
		t.Fatal("pgconn retry trait As target changed")
	}
}
