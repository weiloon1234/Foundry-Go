package failover_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/email/failover"
)

type counting struct {
	calls  atomic.Int32
	result error
}

func (c *counting) Send(context.Context, email.Outbound) (email.Receipt, error) {
	c.calls.Add(1)
	return email.Receipt{}, c.result
}

func send(t *testing.T, driver email.Driver) error {
	t.Helper()
	m, err := email.New(driver, nil, email.DefaultConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close(context.Background()) })
	from, err := email.ParseAddress("sender@example.test")
	if err != nil {
		t.Fatal(err)
	}
	to, err := email.ParseAddress("recipient@example.test")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Send(t.Context(), email.NewMessage(from, "Subject", to).Text("Body"), email.SendOptions{})
	return err
}

func TestFailoverMovesOnOnlyAfterKnownNonAcceptance(t *testing.T) {
	for _, test := range []struct {
		name         string
		first        error
		secondCalled bool
		want         email.Kind
	}{
		{"transient", email.Transient, true, ""},
		{"ambiguous", email.Ambiguous, false, email.Ambiguous},
		{"permanent", email.Permanent, false, email.Permanent},
		{"unknown", errors.New("private transport failure"), false, email.Ambiguous},
	} {
		t.Run(test.name, func(t *testing.T) {
			first, second := &counting{result: test.first}, &counting{}
			driver, err := failover.New(first, second)
			if err != nil {
				t.Fatal(err)
			}
			err = send(t, driver)
			if test.want == "" && err != nil || test.want != "" && email.Classification(err) != test.want {
				t.Fatal("wrong failover outcome", err)
			}
			if (second.calls.Load() == 1) != test.secondCalled || first.calls.Load() != 1 {
				t.Fatal("failover order", first.calls.Load(), second.calls.Load())
			}
		})
	}
	first, second := &counting{result: email.Transient}, &counting{result: email.Transient}
	driver, err := failover.New(first, second)
	if err != nil {
		t.Fatal(err)
	}
	if err := send(t, driver); email.Classification(err) != email.Transient {
		t.Fatal("exhausted failover was not transient", err)
	}
}

func TestRoundRobinRotatesStartingTransport(t *testing.T) {
	first, second := &counting{}, &counting{}
	driver, err := failover.RoundRobin(first, second)
	if err != nil {
		t.Fatal(err)
	}
	for range 4 {
		if err := send(t, driver); err != nil {
			t.Fatal(err)
		}
	}
	if first.calls.Load() != 2 || second.calls.Load() != 2 {
		t.Fatal("round robin did not spread sends", first.calls.Load(), second.calls.Load())
	}
	if _, err := failover.New(first); err == nil {
		t.Fatal("single transport accepted")
	}
	if _, err := failover.New(first, first); err == nil {
		t.Fatal("duplicate transport accepted")
	}
}
