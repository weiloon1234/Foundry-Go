package factory

import (
	"context"
	"errors"
	"runtime"
	"slices"
	"sync"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/fault"
)

type record struct{ ID int64 }

func (record) FoundryQuery() query.Query[record] {
	return query.ForModel(query.Define("factory_records", "id", []query.Column{{Name: "id"}}, func(row database.Row) (record, error) {
		var result record
		err := row.Scan(&result.ID)
		return result, err
	}, query.NewModelField("id", codec.Signed[int64](), func(r record) int64 { return r.ID })))
}

type draft struct {
	number  Sequence
	marks   string
	prepare func() error
}

func (d draft) FoundryCreateMutation(query.Mutation[record]) (query.Mutation[record], error) {
	if d.prepare != nil {
		if err := d.prepare(); err != nil {
			return query.Mutation[record]{}, err
		}
	}
	return query.Change(query.Assign[record]("factory_records", "id", codec.Signed[int64](), int64(d.number))), nil
}
func newFactory(t *testing.T) *Factory[record, draft] {
	t.Helper()
	f, err := New[record](func(_ context.Context, n Sequence) (draft, error) { return draft{number: n}, nil })
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestStatesShareSequenceWithoutChangingParent(t *testing.T) {
	f := newFactory(t)
	state := func(mark string) State[draft] {
		return func(_ context.Context, d draft) (draft, error) { d.marks += mark; return d, nil }
	}
	states := []State[draft]{state("a"), state("b")}
	child, err := f.WithStates(states...)
	if err != nil {
		t.Fatal(err)
	}
	states[0] = state("changed")
	other := newFactory(t)
	for i, item := range []*Factory[record, draft]{child, f, child, other} {
		d, err := item.Draft(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		wantNumber := []Sequence{1, 2, 3, 1}[i]
		wantMarks := []string{"ab", "", "ab", ""}[i]
		if d.number != wantNumber || d.marks != wantMarks {
			t.Fatal("factory state or sequence leaked", d.number, d.marks)
		}
	}
}

func TestConcurrentSequenceIsUnique(t *testing.T) {
	f := newFactory(t)
	numbers := make([]Sequence, 128)
	var wg sync.WaitGroup
	for i := range numbers {
		wg.Go(func() {
			d, err := f.Draft(t.Context())
			if err != nil {
				t.Error(err)
				return
			}
			numbers[i] = d.number
		})
	}
	wg.Wait()
	slices.Sort(numbers)
	for i, n := range numbers {
		if n != Sequence(i+1) {
			t.Fatal("sequence reused or omitted")
		}
	}
}

func TestFailedBuildAndStateNeverPublishPartialDraft(t *testing.T) {
	sentinel := errors.New("builder failure")
	for _, kind := range []string{"error", "panic", "goexit", "state"} {
		t.Run(kind, func(t *testing.T) {
			f, err := New[record](func(_ context.Context, n Sequence) (draft, error) {
				if n == 1 {
					switch kind {
					case "error":
						return draft{number: n}, sentinel
					case "panic":
						panic("private payload")
					case "goexit":
						runtime.Goexit()
					}
				}
				return draft{number: n}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if kind == "state" {
				f, err = f.WithStates(func(_ context.Context, d draft) (draft, error) {
					if d.number == 1 {
						return d, sentinel
					}
					return d, nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			d, err := f.Draft(t.Context())
			if err == nil || d.number != 0 {
				t.Fatal("partial failed draft published")
			}
			d, err = f.Draft(t.Context())
			if err != nil || d.number != 2 {
				t.Fatal("failed sequence reused", err)
			}
		})
	}
}

type untouchedWriter struct{ calls int }

func (w *untouchedWriter) Transaction(context.Context, func(*database.Tx) error, ...database.TxOptions) error {
	w.calls++
	return errors.New("unexpected database work")
}

func TestInvalidFactoryAndBatchFailBeforeDatabaseWork(t *testing.T) {
	w := &untouchedWriter{}
	f := newFactory(t)
	if _, err := New[record, draft](nil); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	var zero *Factory[record, draft]
	if _, err := zero.Draft(t.Context()); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := f.WithStates(nil); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	if _, err := f.WithStates(make([]State[draft], MaxStates+1)...); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	for _, n := range []int{-1, MaxCount + 1} {
		if _, err := f.CreateMany(t.Context(), w, n); !errors.Is(err, fault.Invalid) {
			t.Fatal(err)
		}
	}
	if _, err := f.Draft(nil); !errors.Is(err, fault.Invalid) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := f.Draft(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := f.CreateMany(ctx, w, 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	items, err := f.CreateMany(t.Context(), w, 0)
	if err != nil || len(items) != 0 {
		t.Fatal(err)
	}
	if w.calls != 0 || f.sequence.last != 0 {
		t.Fatal("invalid/empty input performed work")
	}
	f.sequence.last = ^Sequence(0)
	if _, err := f.Draft(t.Context()); !errors.Is(err, fault.Invalid) {
		t.Fatal("exhausted counter wrapped", err)
	}
}

func TestDraftPreparationFailureDoesNotStartBatch(t *testing.T) {
	w := &untouchedWriter{}
	sentinel := errors.New("draft preparation")
	f, err := New[record](func(_ context.Context, n Sequence) (draft, error) {
		return draft{number: n, prepare: func() error {
			if n == 2 {
				return sentinel
			}
			return nil
		}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err := f.CreateMany(t.Context(), w, 3)
	if !errors.Is(err, sentinel) || len(items) != 0 || w.calls != 0 {
		t.Fatal("preparation failure reached database", err)
	}
}
