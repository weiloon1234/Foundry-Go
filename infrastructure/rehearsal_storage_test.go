package infrastructure_test

import (
	"github.com/weiloon1234/Foundry-Go/infrastructure"
	"strings"
	"testing"
)

func TestOperatorConfigurationCannotEnableRehearsal(t *testing.T) {
	const declaration = `{"uploads":{"driver":"s3_compatible","cloud":{"conditional_write_mode":"rehearsal_runtime","compatible":{"conditional_create":true,"single_request_conditions":true}}}}`
	for _, id := range []string{"", strings.Repeat("a", 32)} {
		t.Setenv("FOUNDRY_REHEARSAL_RUN_ID", id)
		var disks infrastructure.Disks
		if err := disks.UnmarshalText([]byte(declaration)); err == nil || len(disks) != 0 {
			t.Fatal("retired operator key accepted", err)
		}
	}
}
