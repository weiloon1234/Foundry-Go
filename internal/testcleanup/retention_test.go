package testcleanup

import "testing"

func TestKeepRequiresExplicitOptIn(t *testing.T) {
	for _, value := range []string{"", "0", "true", "1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("FOUNDRY_TEST_RETENTION_PROBE", value)
			if got := Keep(t, "FOUNDRY_TEST_RETENTION_PROBE"); got != (value == "1") {
				t.Fatalf("Keep=%v for %q", got, value)
			}
		})
	}
}

func TestKeepDuringPanicUnwind(t *testing.T) {
	t.Setenv("FOUNDRY_TEST_RETENTION_PROBE", "0")
	kept := false
	func() {
		defer func() { _ = recover() }()
		defer func() { kept = Keep(t, "FOUNDRY_TEST_RETENTION_PROBE") }()
		panic("retention probe")
	}()
	if !kept {
		t.Fatal("panic unwinding did not retain test data")
	}
}
