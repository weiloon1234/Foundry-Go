package lifecycle_test

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
	"github.com/weiloon1234/Foundry-Go/value"
)

type privateComparisonError struct{ modelValue string }

func (e *privateComparisonError) Error() string { return "invalid snapshot: " + e.modelValue }
func TestComparisonErrorsRetainCauseWithoutFormattingSnapshots(t *testing.T) {
	const modelValue = "private-shared-comparison-value"
	cause := &privateComparisonError{modelValue: modelValue}
	c := codec.New(func(v string) (driver.Value, error) {
		if v == modelValue {
			return nil, cause
		}
		return v, nil
	}, func(raw any) (string, error) { return raw.(string), nil })
	for _, policy := range []bool{false, true} {
		for _, side := range []string{"original", "resulting"} {
			t.Run(fmt.Sprint(policy)+"/"+side, func(t *testing.T) {
				before, after := "ordinary", "ordinary"
				if side == "original" {
					before = modelValue
				} else {
					after = modelValue
				}
				var err error
				if policy {
					_, err = lifecycle.CompareFieldPolicy(lifecycle.FieldPolicyOf(c), value.Set(before), value.Set(after), true)
				} else {
					_, err = lifecycle.CompareField(c, value.Set(before), value.Set(after), true)
				}
				if !errors.Is(err, cause) {
					t.Fatal("comparison lost cause")
				}
				var got *privateComparisonError
				if !errors.As(err, &got) || got != cause {
					t.Fatal("comparison lost typed cause")
				}
				for _, formatted := range []string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
					if strings.Contains(formatted, modelValue) {
						t.Fatal("comparison diagnostic exposed a model value")
					}
				}
			})
		}
	}
}
