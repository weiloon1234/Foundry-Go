package email_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"testing"

	"github.com/weiloon1234/Foundry-Go/email"
	"github.com/weiloon1234/Foundry-Go/storage"
)

// The escape makes an old unbounded traversal fail an assertion without hanging
// the test process. Error must never be called while inspecting private causes.
type cyclicEmailError struct{ visits atomic.Int32 }

func (*cyclicEmailError) Error() string { panic("private email error formatted") }
func (e *cyclicEmailError) Unwrap() error {
	if e.visits.Add(1) > 2048 {
		return nil
	}
	return e
}

func TestClassificationPreservesRetryPriorityAndRejectsIncompleteGraphs(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want email.Kind
	}{
		{"nil", nil, ""},
		{"unknown", errors.New("private"), email.Ambiguous},
		{"wrapped-transient", fmt.Errorf("temporary: %w", email.Transient), email.Transient},
		{"construction-over-transient", errors.Join(email.Transient, email.Construction), email.Construction},
		{"permanent-over-construction", errors.Join(email.Construction, email.Permanent), email.Permanent},
		{"ambiguous-over-permanent", errors.Join(email.Permanent, email.Ambiguous), email.Ambiguous},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := email.Classification(test.err); got != test.want {
				t.Fatalf("classification = %q, want %q", got, test.want)
			}
		})
	}
	for _, prior := range []error{nil, email.Transient, email.Permanent} {
		cycle := &cyclicEmailError{}
		if got := email.Classification(errors.Join(prior, cycle)); got != email.Ambiguous {
			t.Fatalf("incomplete graph classified as %q", got)
		}
		if cycle.visits.Load() == 0 || cycle.visits.Load() > 256 {
			t.Fatal("error inspection was not bounded", cycle.visits.Load())
		}
	}
}

func TestCyclicDriverFailureReleasesSendAndShutdownOwnership(t *testing.T) {
	cycle := &cyclicEmailError{}
	var calls atomic.Int32
	m := mailer(t, email.DriverFunc(func(context.Context, email.Outbound) (email.Receipt, error) {
		if calls.Add(1) == 1 {
			return email.Receipt{}, cycle
		}
		return email.Receipt{}, nil
	}), nil, nil)
	result, err := m.Send(t.Context(), message(t), email.SendOptions{})
	if err != email.Ambiguous || result.Accepted || m.Snapshot().Active != 0 {
		t.Fatal("cyclic driver failure retained ownership or claimed acceptance")
	}
	if cycle.visits.Load() == 0 || cycle.visits.Load() > 256 {
		t.Fatal("driver error inspection was not bounded", cycle.visits.Load())
	}
	result, err = m.Send(t.Context(), message(t), email.SendOptions{})
	if err != nil || !result.Accepted {
		t.Fatal("later valid send failed")
	}
	if err := m.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-m.Done():
	default:
		t.Fatal("mailer did not finish shutdown")
	}
}

type cyclicAttachmentBackend struct {
	storage.Backend
	failure error
}

func (b cyclicAttachmentBackend) Open(context.Context, storage.ObjectKey, storage.ReadOptions) (io.ReadCloser, storage.ReadInfo, error) {
	return nil, storage.ReadInfo{}, storage.Failure(storage.Unavailable, storage.OpenOperation, storage.NotApplicable, b.failure)
}
func TestCyclicAttachmentFailureDoesNotSubmitOrRetainCapacity(t *testing.T) {
	cycle := &cyclicEmailError{}
	registry, attachment := attachmentStore(t, func(backend storage.Backend) storage.Backend {
		return cyclicAttachmentBackend{Backend: backend, failure: cycle}
	})
	var calls atomic.Int32
	m := mailer(t, email.DriverFunc(func(context.Context, email.Outbound) (email.Receipt, error) {
		calls.Add(1)
		return email.Receipt{}, nil
	}), registry, nil)
	result, err := m.Send(t.Context(), message(t).Attach(attachment), email.SendOptions{})
	if err != email.Construction || result.Accepted || calls.Load() != 0 || m.Snapshot().Active != 0 {
		t.Fatal("unclassifiable attachment failure submitted or retained send")
	}
	if cycle.visits.Load() == 0 || cycle.visits.Load() > 256 {
		t.Fatal("attachment inspection was not bounded", cycle.visits.Load())
	}
	disk, err := registry.Disk(attachment.Disk)
	if err != nil {
		t.Fatal(err)
	}
	if disk.Stats().Active != 0 {
		t.Fatal("attachment failure retained disk")
	}
	result, err = m.Send(t.Context(), message(t), email.SendOptions{})
	if err != nil || !result.Accepted {
		t.Fatal("later attachment-free send failed")
	}
}
