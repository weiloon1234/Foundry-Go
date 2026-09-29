package notifications_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/notifications"
	notificationtest "github.com/weiloon1234/Foundry-Go/testkit/notifications"
)

type Receipt struct {
	Order string `json:"order"`
}

type failures struct{ messages []string }

func (*failures) Helper() {}
func (f *failures) Errorf(format string, args ...any) {
	f.messages = append(f.messages, fmt.Sprintf(format, args...))
}

func TestRecordingTransportAssertsTypedOutputsPrivately(t *testing.T) {
	transport := notificationtest.New[Receipt](t, 4)
	id, err := notifications.ParseDeliveryID(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	if outcome, err := transport.Deliver(t.Context(), id, Receipt{Order: "A-1"}); err != nil || outcome != notifications.Accepted {
		t.Fatal(outcome, err)
	}
	transport.Respond(notifications.Retry)
	if outcome, err := transport.Deliver(t.Context(), id, Receipt{Order: "A-2"}); err != nil || outcome != notifications.Retry {
		t.Fatal(outcome, err)
	}
	isFirst := func(r Receipt) bool { return r.Order == "A-1" }
	notificationtest.AssertDelivered(t, transport, isFirst)
	notificationtest.AssertDeliveredCount(t, transport, nil, 1)
	notificationtest.AssertNotDelivered(t, transport, func(r Receipt) bool { return r.Order == "A-2" })
	recorded := &failures{}
	notificationtest.AssertNotDelivered(recorded, transport, isFirst)
	if len(recorded.messages) != 1 || strings.Contains(recorded.messages[0], "A-1") {
		t.Fatal("assertion failure missing or exposed an output", recorded.messages)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if outcome, _ := transport.Deliver(cancelled, id, Receipt{}); outcome != notifications.Retry {
		t.Fatal("cancelled delivery was not a definite non-acceptance")
	}
}
