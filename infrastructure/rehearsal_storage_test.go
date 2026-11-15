package infrastructure_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"github.com/weiloon1234/Foundry-Go/storage"
)

func TestRehearsalStorageParsingRequiresBothSignals(t *testing.T) {
	const declaration = `{"uploads":{"driver":"s3_compatible","cloud":{"conditional_write_mode":"rehearsal_runtime","compatible":{"conditional_create":true,"single_request_conditions":true}}}}`
	for _, runID := range []string{"", "not-a-run", strings.Repeat("a", 32)} {
		t.Run(runID, func(t *testing.T) {
			t.Setenv("FOUNDRY_REHEARSAL_RUN_ID", runID)
			var disks infrastructure.Disks
			err := disks.UnmarshalText([]byte(declaration))
			if len(runID) == 32 {
				if err != nil || disks["uploads"].Cloud.ConditionalWriteMode != storage.RehearsalRuntime {
					t.Fatal(err)
				}
			} else if !errors.Is(err, storage.Invalid) || len(disks) != 0 {
				t.Fatal("invalid identity accepted", err)
			}
		})
	}
	t.Setenv("FOUNDRY_REHEARSAL_RUN_ID", strings.Repeat("a", 32))
	for _, invalid := range []string{
		strings.Replace(declaration, "s3_compatible", "r2", 1),
		strings.Replace(declaration, `"single_request_conditions":true`, `"single_request_conditions":false`, 1),
		strings.Replace(declaration, "rehearsal_runtime", "unknown", 1),
	} {
		var disks infrastructure.Disks
		if err := disks.UnmarshalText([]byte(invalid)); !errors.Is(err, storage.Invalid) {
			t.Fatal("invalid marker accepted", err)
		}
	}
	var disks infrastructure.Disks
	if err := disks.UnmarshalText([]byte(`{"uploads":{"driver":"s3_compatible","cloud":{"compatible":{"conditional_create":true,"single_request_conditions":true}}}}`)); err != nil {
		t.Fatal(err)
	}
	if disks["uploads"].Cloud.ConditionalWriteMode != "" {
		t.Fatal("run identity alone enabled reduced fidelity")
	}
}
