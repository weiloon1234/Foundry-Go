package query

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type privateQueryBindingError struct{ formatted int }

func (e *privateQueryBindingError) Error() string { e.formatted++; return "private-query-binding" }
func (e *privateQueryBindingError) Format(s fmt.State, _ rune) {
	e.formatted++
	fmt.Fprint(s, "private-query-formatter")
}

func TestQueryRedactsCompiledAndCapturedBindingErrors(t *testing.T) {
	for _, kind := range []string{"codec", "validator"} {
		t.Run(kind, func(t *testing.T) {
			cause := &privateQueryBindingError{}
			c := codec.New(func(int64) (driver.Value, error) { return nil, cause }, func(any) (int64, error) { return 0, nil }).WithParameterType(codec.TypeInteger)
			if kind == "validator" {
				c = codec.Signed[int64]().Validated(func(int64) error { return cause })
			}
			field := NewScalarField[cursorRecord, int64]("records", "id", c)
			for _, tc := range []struct {
				name      string
				predicate Predicate[cursorRecord]
			}{
				{"compiled", field.Eq(1)},
				{"captured", Value(field).Param(1).IsNull()},
			} {
				t.Run(tc.name, func(t *testing.T) {
					q := cursorQuery().Where(tc.predicate)
					_, compileErr := q.Compile()
					_, readErr := q.All(t.Context(), refusingExecutor{})
					for _, err := range []error{compileErr, readErr} {
						var retained *privateQueryBindingError
						if !errors.Is(err, cause) || errors.Is(err, fault.Invalid) || !errors.As(err, &retained) || retained != cause {
							t.Fatal("query lost binding cause")
						}
						for _, message := range []string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err), fmt.Errorf("query failed: %w", err).Error()} {
							if strings.Contains(message, "private-query") {
								t.Fatal("query disclosed private binding data")
							}
						}
					}
					if cause.formatted != 0 {
						t.Fatal("query formatted its binding cause")
					}
				})
			}
		})
	}
}
