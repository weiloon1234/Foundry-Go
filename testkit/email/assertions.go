// Package email provides assertions for the opt-in memory email driver. Failure
// text deliberately excludes private message contents and recipient addresses.
package email

import (
	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/email/memory"
)

type TestingT interface {
	Helper()
	Errorf(string, ...any)
}

func AssertCount(t TestingT, driver *memory.Driver, want int) {
	t.Helper()
	if got := len(driver.Messages()); got != want {
		t.Errorf("recorded email count: got %d, want %d", got, want)
	}
}
func AssertRecipientCount(t TestingT, driver *memory.Driver, recipient email.Address, want int) {
	t.Helper()
	got := 0
	for _, out := range driver.Messages() {
		for _, a := range out.Message().EnvelopeRecipients() {
			if a.Mailbox() == recipient.Mailbox() {
				got++
				break
			}
		}
	}
	if got != want {
		t.Errorf("email recipient match count: got %d, want %d", got, want)
	}
}
