package jobs_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/jobs"
	"github.com/weiloon1234/Foundry-Go/testkit"
	jobstest "github.com/weiloon1234/Foundry-Go/testkit/jobs"
)

type Work struct {
	Number int `json:"number"`
}

func TestMemoryHarnessKeepsTypedRegistrationDeduplicationAndIsolation(t *testing.T) {
	source := testkit.NewClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	definition := jobs.Define[Work]("test.work", 1, jobs.DefaultPolicy("default"))
	declaration, err := definition.Declare(nil)
	if err != nil {
		t.Fatal(err)
	}
	var retained *jobstest.Harness
	var id jobs.ID[Work]
	t.Run("owner", func(t *testing.T) {
		retained = jobstest.New(t, source, declaration)
		other := jobstest.New(t, source, declaration)
		if retained.Namespace == other.Namespace {
			t.Fatal("test queue namespace reused")
		}
		pending, err := definition.Capture(t.Context(), Work{Number: 7}, jobs.Options[Work]{})
		if err != nil {
			t.Fatal(err)
		}
		id = pending.ID()
		first, err := pending.Dispatch(t.Context(), retained.Dispatcher)
		if err != nil || !first.Inserted {
			t.Fatal(err)
		}
		second, err := pending.Dispatch(t.Context(), retained.Dispatcher)
		if err != nil || second.Inserted {
			t.Fatal("duplicate dispatch changed acceptance", err)
		}
		jobstest.AssertState(t, definition, retained.Dispatcher, id, "", jobs.Waiting)
		found, err := definition.Inspect(t.Context(), other.Dispatcher, id, "")
		if err != nil || found.IsSet() {
			t.Fatal("separate test backend shared records", err)
		}
		wrong := jobs.Define[Work]("test.undeclared", 1, jobs.DefaultPolicy("default"))
		if _, err := wrong.Dispatch(t.Context(), retained.Dispatcher, Work{}, jobs.Options[Work]{}); err == nil {
			t.Fatal("test bypassed registration")
		}
	})
	if _, err := definition.Inspect(t.Context(), retained.Dispatcher, id, ""); !errors.Is(err, fault.Closed) {
		t.Fatal("cleanup did not close backend", err)
	}
}

// failures records assertion failures instead of failing the enclosing test.
type failures struct {
	testing.TB
	messages []string
}

func (f *failures) Errorf(format string, args ...any) {
	f.messages = append(f.messages, fmt.Sprintf(format, args...))
}

func TestPushedAssertionsMatchTypedPayloadsWithoutPrintingThem(t *testing.T) {
	definition := jobs.Define[Work]("test.pushed", 1, jobs.DefaultPolicy("default"))
	declaration, err := definition.Declare(nil)
	if err != nil {
		t.Fatal(err)
	}
	h := jobstest.New(t, testkit.NewClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)), declaration)
	for _, number := range []int{41, 42} {
		if _, err := definition.Dispatch(t.Context(), h.Dispatcher, Work{Number: number}, jobs.Options[Work]{}); err != nil {
			t.Fatal(err)
		}
	}
	answer := func(w Work) bool { return w.Number == 42 }
	jobstest.AssertPushed(t, h.Dispatcher, definition, "", answer)
	jobstest.AssertNotPushed(t, h.Dispatcher, definition, "", func(w Work) bool { return w.Number == 7 })
	jobstest.AssertPushedCount(t, h.Dispatcher, definition, "", nil, 2)
	recorded := &failures{TB: t}
	jobstest.AssertNotPushed(recorded, h.Dispatcher, definition, "", answer)
	jobstest.AssertPushedCount(recorded, h.Dispatcher, definition, "", answer, 2)
	if len(recorded.messages) != 2 || strings.Contains(strings.Join(recorded.messages, " "), "42") {
		t.Fatal("assertion failures missing or exposed a payload", recorded.messages)
	}
}
