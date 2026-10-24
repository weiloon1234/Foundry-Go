package query

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/querycore"
	"github.com/weiloon1234/Foundry-Go/internal/sqlowner"
)

type privateLookupError struct{ value string }

func (e *privateLookupError) Error() string { return e.value }
func assertPrivateLookupError(t *testing.T, err error, cause *privateLookupError) {
	t.Helper()
	var typed *privateLookupError
	if !errors.Is(err, cause) || !errors.As(err, &typed) || typed != cause {
		t.Fatal("lost error identity")
	}
	for _, s := range []string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
		if strings.Contains(s, cause.value) {
			t.Fatal("private error formatted")
		}
	}
}
func TestExplainCompilationErrorsArePrivateAcrossFrontends(t *testing.T) {
	cause := &privateLookupError{value: "private-codec-binding"}
	executor := &planExecutor{}
	c := codec.New(func(int64) (driver.Value, error) { return nil, cause }, func(any) (int64, error) { return 0, nil })
	field := NewScalarField[cursorRecord, int64]("records", "id", c)
	_, err := cursorQuery().Where(field.Eq(1)).Explain(t.Context(), executor)
	assertPrivateLookupError(t, err, cause)
	_, err = FoundryExplain(t.Context(), executor, func(context.Context) (querycore.Statement, error) { return querycore.Statement{}, cause }, false, sqlowner.Queries())
	assertPrivateLookupError(t, err, cause)
	if executor.calls != 0 {
		t.Fatal("failed compile reached executor")
	}
}
func TestOldLookupDraftErrorsLeaveBoundarySafely(t *testing.T) {
	cause := &privateLookupError{value: "private-draft-model"}
	err := callback.Isolated("model draft", func() error {
		_, err := modelUpdateMutation(t.Context(), nil, cursorRecord{}, func(context.Context, *database.Tx, cursorRecord) (Mutation[cursorRecord], error) {
			return Mutation[cursorRecord]{}, cause
		})
		return err
	})
	assertPrivateLookupError(t, err, cause)
}
