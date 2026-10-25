package query

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/value"
)

func TestCursorCodecRedactionPreservesClassification(t *testing.T) {
	first, err := cursorQuery().cursorPlan(CursorRequest[cursorRecord]{Size: 2})
	if err != nil {
		t.Fatal(err)
	}
	token, err := makeCursor(first.scope, first.fields, cursorRecord{ID: 7})
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"encode", "decode", "validate"} {
		for _, invalid := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/invalid=%v", phase, invalid), func(t *testing.T) {
				private := &privateQueryBindingError{}
				var cause error = private
				if invalid {
					cause = fault.Wrap(fault.Invalid, "invalid cursor key", private)
				}
				c := codec.New(func(v int64) (driver.Value, error) {
					if phase == "encode" {
						return nil, cause
					}
					return v, nil
				}, func(any) (int64, error) {
					if phase == "decode" {
						return 0, cause
					}
					return 7, nil
				})
				if phase == "validate" {
					c = c.Validated(func(int64) error { return cause })
				}
				field := NewRecordField("id", c, func(r cursorRecord) int64 { return r.ID })
				_, err := readCursorBoundary(CursorRequest[cursorRecord]{Size: 1, After: value.Set(token)}, first.scope, []RecordField[cursorRecord]{field}, []bool{true})
				var input *CursorInputError
				var retained *privateQueryBindingError
				if err == nil || errors.As(err, &input) != invalid || errors.Is(err, fault.Invalid) != invalid || !errors.Is(err, private) || !errors.As(err, &retained) || retained != private {
					t.Fatal("cursor codec classification or cause changed")
				}
				_ = fmt.Sprintf("%v %+v %#v", err, err, err)
				if private.formatted != 0 {
					t.Fatal("cursor codec error was formatted")
				}
			})
		}
	}
}
