package rows

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/orm/expr"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type privateQueryBindingError struct{ formatted int }

func (e *privateQueryBindingError) Error() string { e.formatted++; return "private-query-binding" }
func (e *privateQueryBindingError) Format(s fmt.State, _ rune) {
	e.formatted++
	fmt.Fprint(s, "private-query-formatter")
}

func TestQueryRedactsCompiledAndCapturedBindingErrors(t *testing.T) {
	type item struct {
		ID     int64
		Number int64
	}
	type fields struct {
		orm.Table[item, int64]
		Number orm.Number[item, int64]
	}
	for _, kind := range []string{"codec", "validator"} {
		t.Run(kind, func(t *testing.T) {
			cause := &privateQueryBindingError{}
			c := codec.New(func(int64) (driver.Value, error) { return nil, cause }, func(any) (int64, error) { return 0, nil }).WithParameterType(codec.TypeInteger)
			if kind == "validator" {
				c = codec.Signed[int64]().Validated(func(int64) error { return cause })
			}
			f := orm.Define("binding_records", func(b orm.Builder[item], r *item) fields {
				return fields{orm.TableOf(b, &r.ID), orm.NumberOf(b, &r.Number, orm.Codec(c))}
			})
			for name, q := range map[string]orm.Query[item]{"compiled": f.Query().Where(orm.NumberEq(f.Number, 1)), "captured": expr.Where(f.Query(), expr.Param(orm.NumberExpr(f.Number), int64(1)).IsNull())} {
				t.Run(name, func(t *testing.T) {
					state := &driverState{}
					_, compileErr := orm.Compile(q)
					_, readErr := orm.All(t.Context(), openTest(t, state), q)
					for _, err := range []error{compileErr, readErr} {
						var retained *privateQueryBindingError
						if !errors.Is(err, cause) || errors.Is(err, fault.Invalid) || !errors.As(err, &retained) || retained != cause {
							t.Fatal("query lost binding cause")
						}
						for _, message := range []string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err), fmt.Errorf("query failed: %w", err).Error()} {
							if strings.Contains(message, "private-query") {
								t.Fatal("query disclosed binding data")
							}
						}
					}
					if cause.formatted != 0 || state.sql != "" {
						t.Fatal("formatted cause or executed failed binding", cause.formatted, state.sql)
					}
				})
			}
		})
	}
}
