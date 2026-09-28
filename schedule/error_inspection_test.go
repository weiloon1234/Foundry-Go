package schedule_test

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/schedule"
)

type inspectionError struct{ inspect func() }

func (inspectionError) Error() string   { return "private domain error" }
func (e inspectionError) Is(error) bool { e.inspect(); return false }

func TestAbnormalErrorInspectionRemainsOwnedAndIsolated(t *testing.T) {
	for _, abnormal := range []struct {
		name    string
		inspect func()
	}{{"panic", func() { panic("private inspection data") }}, {"goexit", runtime.Goexit}} {
		for _, overlap := range []bool{false, true} {
			for _, failureHook := range []bool{false, true} {
				name := abnormal.name
				if overlap {
					name += "/overlap"
				}
				if failureHook {
					name += "/hook"
				}
				t.Run(name, func(t *testing.T) {
					options := schedule.DefaultOptions()
					options.WithoutOverlap = overlap
					handler := func(context.Context, schedule.Invocation) error { return inspectionError{abnormal.inspect} }
					if failureHook {
						handler = func(context.Context, schedule.Invocation) error { return errors.New("ordinary failure") }
						options.Failed = func(context.Context, schedule.Invocation, error) error { return inspectionError{abnormal.inspect} }
					}
					f := newFixture(t,
						every(t, "abnormal", handler, options),
						every(t, "healthy", func(context.Context, schedule.Invocation) error { return nil }),
					)
					f.start(t)
					for round := 1; round <= 2; round++ {
						f.advance(time.Minute)
						waitFor(t, func() bool {
							snapshot := f.scheduler.Snapshot()
							return snapshot.Active == 0 && len(snapshot.History) == round*2
						})
					}
					for _, record := range f.scheduler.Snapshot().History {
						if record.Invocation.Schedule == "abnormal" {
							if record.State != schedule.Failed || record.Reason != schedule.Panicked {
								t.Fatal("abnormal inspection escaped classification")
							}
						} else if record.State != schedule.Succeeded {
							t.Fatal("unrelated schedule was stopped")
						}
					}
				})
			}
		}
	}
}
