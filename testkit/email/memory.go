package email

import (
	"github.com/weiloon1234/Foundry-Go/email/memory"
	"testing"
)

// New supplies a fresh bounded production memory driver and owns its cleanup.
// Pass it explicitly to the normal mailer; no external delivery is attempted.
func New(t testing.TB, capacity int) *memory.Driver {
	t.Helper()
	driver, err := memory.New(capacity)
	if err != nil {
		t.Fatalf("create test mail driver: %v", err)
	}
	t.Cleanup(driver.Close)
	return driver
}
