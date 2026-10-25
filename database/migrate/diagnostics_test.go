package migrate

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/callback"
	"github.com/weiloon1234/Foundry-Go/internal/safeerror"
)

func TestMigrationActionDiagnosticSurvivesContainment(t *testing.T) {
	p := &Postgres{config: DefaultPostgresConfig()}
	for _, test := range []struct {
		name string
		code database.Code
		want string
	}{
		{"classified", database.QueryFailed, "consumer: invalid: migration app/0002_second failed and was not committed (query_failed)"},
		{"owned classification", database.QueryFailed, "consumer: invalid: migration app/0002_second failed and was not committed (query_failed)"},
		{"application", "", "consumer failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cause := &migrationInspectionError{}
			var input error = cause
			var classified error
			if test.code != "" {
				classified = database.NewError("execute", test.code)
				input = errors.Join(classified, cause)
			}
			if test.name == "owned classification" {
				input = safeerror.Owned("database execute: query_failed", input)
			}
			result := callback.Isolated("consumer", func() error {
				return p.failure(Key{Origin: "app", ID: "0002_second"}, input)
			})
			var retained *migrationInspectionError
			if result == nil || result.Error() != test.want || !errors.Is(result, input) || !errors.Is(result, cause) || !errors.As(result, &retained) || retained != cause {
				t.Fatal("migration lost its action diagnostic or cause identity")
			}
			if classified != nil && (!errors.Is(result, classified) || !errors.Is(result, test.code) || !errors.Is(result, fault.Invalid)) {
				t.Fatal("migration classification changed")
			}
			if fmt.Sprintf("%v %+v %#v", result, result, result) != test.want+" "+test.want+" "+test.want || cause.formatted != 0 {
				t.Fatal("migration invoked application formatting")
			}
		})
	}
}

func TestMigrationDoesNotPromotePrivateClassificationText(t *testing.T) {
	p := &Postgres{config: DefaultPostgresConfig()}
	original := database.NewError("password=hunter2", database.Code("password=hunter2"))
	result := p.failure(Key{Origin: "app", ID: "0003_private"}, original)
	if strings.Contains(result.Error(), "hunter2") || !errors.Is(result, original) || !errors.Is(result, database.Code("password=hunter2")) {
		t.Fatal("migration promoted private adapter classification or lost identity")
	}
}
