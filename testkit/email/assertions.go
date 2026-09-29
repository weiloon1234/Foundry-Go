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

func countSent(driver *memory.Driver, match func(email.Message) bool) int {
	got := 0
	for _, out := range driver.Messages() {
		if match == nil || match(out.Message()) {
			got++
		}
	}
	return got
}

// AssertSent requires at least one recorded message satisfying match (nil
// matches any). Failure text never includes message content or addresses.
func AssertSent(t TestingT, driver *memory.Driver, match func(email.Message) bool) {
	t.Helper()
	if countSent(driver, match) == 0 {
		t.Errorf("no matching email was sent")
	}
}

// AssertNotSent requires that no recorded message satisfies match.
func AssertNotSent(t TestingT, driver *memory.Driver, match func(email.Message) bool) {
	t.Helper()
	if got := countSent(driver, match); got != 0 {
		t.Errorf("%d matching email(s) were sent", got)
	}
}

// AssertSentCount requires exactly want recorded messages satisfying match.
func AssertSentCount(t TestingT, driver *memory.Driver, match func(email.Message) bool, want int) {
	t.Helper()
	if got := countSent(driver, match); got != want {
		t.Errorf("matching email count: got %d, want %d", got, want)
	}
}
