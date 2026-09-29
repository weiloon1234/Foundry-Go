package factory

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/fault"
)

func TestPerCallOverridesAndHooksDoNotChangeTheFactory(t *testing.T) {
	f := newFactory(t)
	override := func(_ context.Context, d draft) (draft, error) { d.marks += "override"; return d, nil }
	drafted, err := f.DraftWith(t.Context(), override)
	if err != nil || drafted.marks != "override" || drafted.number != 1 {
		t.Fatalf("override draft = %+v, %v", drafted, err)
	}
	plain, err := f.Draft(t.Context())
	if err != nil || plain.marks != "" || plain.number != 2 {
		t.Fatalf("override was retained or sequence was not shared: %+v, %v", plain, err)
	}
	hook := func(_ context.Context, _ database.Transactor, r record) (record, error) { return r, nil }
	hooked, err := f.AfterCreating(hook)
	if err != nil || len(hooked.after) != 1 || len(f.after) != 0 {
		t.Fatal("AfterCreating changed its source factory", err)
	}
	if _, err := hooked.Create(t.Context(), nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("hooked create without a writer", err)
	}
	if _, err := hooked.CreateMany(t.Context(), nil, 1); !errors.Is(err, fault.Invalid) {
		t.Fatal("hooked batch without a writer", err)
	}
	if _, err := f.AfterCreating(nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("nil hook accepted", err)
	}
	if _, err := f.AfterCreating(slices.Repeat([]AfterCreate[record]{hook}, MaxHooks+1)...); !errors.Is(err, fault.Invalid) {
		t.Fatal("unbounded hooks accepted", err)
	}
	if _, _, err := CreateFor(t.Context(), nil, f, f, nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("relationship without assignment accepted", err)
	}
	if _, _, err := CreateHas(t.Context(), nil, f, f, 1, nil); !errors.Is(err, fault.Invalid) {
		t.Fatal("relationship without assignment accepted", err)
	}
}

func TestAfterCreateHooksRunInOrderAndStopOnFailure(t *testing.T) {
	f := newFactory(t)
	var calls []string
	failure := errors.New("hook failed")
	hooked, err := f.AfterCreating(
		func(_ context.Context, _ database.Transactor, r record) (record, error) {
			calls = append(calls, "first")
			r.ID++
			return r, nil
		},
		func(_ context.Context, _ database.Transactor, r record) (record, error) {
			calls = append(calls, "second")
			return r, failure
		},
		func(_ context.Context, _ database.Transactor, r record) (record, error) {
			calls = append(calls, "third")
			return r, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hooked.afterCreate(t.Context(), nil, record{ID: 1}); !errors.Is(err, failure) || !slices.Equal(calls, []string{"first", "second"}) {
		t.Fatalf("hooks = %v, %v", calls, err)
	}
	panicking, err := f.AfterCreating(func(context.Context, database.Transactor, record) (record, error) { panic("hook panic") })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := panicking.afterCreate(t.Context(), nil, record{}); err == nil {
		t.Fatal("hook panic escaped isolation")
	}
}

func TestFakerIsDeterministicAndBounded(t *testing.T) {
	first, second := NewFaker(42), NewFaker(42)
	for range 32 {
		a := []string{first.Name(), first.Email(), first.Word(), first.Sentence()}
		b := []string{second.Name(), second.Email(), second.Word(), second.Sentence()}
		if !slices.Equal(a, b) {
			t.Fatalf("same seed diverged: %v != %v", a, b)
		}
	}
	faker := NewFaker(7)
	emails := make(map[string]bool)
	for range 200 {
		email := faker.Email()
		if emails[email] || !strings.HasSuffix(email, "@example.test") {
			t.Fatalf("email %q repeated or left the reserved domain", email)
		}
		emails[email] = true
		if n := faker.IntBetween(-3, 3); n < -3 || n > 3 {
			t.Fatalf("IntBetween escaped its range: %d", n)
		}
		if sentence := faker.Sentence(); !strings.HasSuffix(sentence, ".") || strings.ToUpper(sentence[:1]) != sentence[:1] {
			t.Fatalf("malformed sentence %q", sentence)
		}
	}
	faker.IntBetween(-1<<63, 1<<63-1)
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	if at := faker.TimeBetween(from, to); at.Before(from) || !at.Before(to) || at.Nanosecond()%1000 != 0 {
		t.Fatalf("TimeBetween = %v", at)
	}
	if got := len(faker.Words(3)); got != 3 {
		t.Fatalf("Words returned %d words", got)
	}
}
