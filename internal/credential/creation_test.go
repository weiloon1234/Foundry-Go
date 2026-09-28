package credential

import (
	"context"
	"errors"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
)

func TestCheckedCreationRejectsOmittedRepeatedAndSuppressedChecks(t *testing.T) {
	for _, mode := range []string{"pass", "missing", "duplicate", "suppressed", "backend-error"} {
		t.Run(mode, func(t *testing.T) {
			cause := errors.New("check rejected")
			calls := 0
			result, err := CheckCreation(t.Context(), func(context.Context, *database.Tx) error {
				calls++
				if mode == "suppressed" {
					return cause
				}
				return nil
			}, func(check func(context.Context, *database.Tx) error) (int, error) {
				if mode == "missing" {
					return 42, nil
				}
				_ = check(t.Context(), &database.Tx{})
				if mode == "duplicate" {
					_ = check(t.Context(), &database.Tx{})
				}
				if mode == "backend-error" {
					return 42, cause
				}
				return 42, nil
			})
			if mode == "pass" {
				if err != nil || result != 42 || calls != 1 {
					t.Fatal(result, err)
				}
				return
			}
			if err == nil || result != 0 {
				t.Fatal("bad adapter published result")
			}
			if (mode == "suppressed" || mode == "backend-error") && !errors.Is(err, cause) {
				t.Fatal("cause lost", err)
			}
		})
	}
}
