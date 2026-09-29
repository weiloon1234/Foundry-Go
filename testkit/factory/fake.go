package factory

import (
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Faker produces deterministic synthetic test values from a seed, so a failing
// test can be reproduced. It is safe for concurrent use; concurrent callers
// share one stream, so their interleaving decides which values each receives.
// Values are fictional and use reserved example domains; never production data.
type Faker struct {
	mu     sync.Mutex
	random *rand.Rand
	emails uint64
}

// NewFaker returns an independent generator for seed.
func NewFaker(seed uint64) *Faker {
	return &Faker{random: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))}
}

var (
	fakeFirstNames = []string{"Ada", "Alan", "Amara", "Chen", "Diego", "Farah", "Grace", "Hana", "Ivan", "Jonas", "Kofi", "Lina", "Maya", "Noah", "Omar", "Priya", "Rosa", "Sven", "Tomas", "Yuki"}
	fakeLastNames  = []string{"Abara", "Bauer", "Costa", "Dubois", "Evans", "Fischer", "Garcia", "Haddad", "Ito", "Jensen", "Khan", "Lopez", "Moreau", "Nakamura", "Okafor", "Petrov", "Quinn", "Rossi", "Silva", "Tan"}
	fakeWords      = []string{"amber", "anchor", "atlas", "beacon", "breeze", "canyon", "cedar", "comet", "delta", "ember", "falcon", "garnet", "harbor", "island", "juniper", "kernel", "lantern", "meadow", "nebula", "orbit", "pebble", "quartz", "river", "summit", "timber", "umbra", "valley", "willow", "yonder", "zephyr"}
)

func (f *Faker) intN(n int) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.random.IntN(n)
}

// Pick returns one of items; it panics when items is empty, like an index.
func Pick[T any](f *Faker, items ...T) T { return items[f.intN(len(items))] }

// FirstName, LastName and Name return fictional personal names.
func (f *Faker) FirstName() string { return Pick(f, fakeFirstNames...) }
func (f *Faker) LastName() string  { return Pick(f, fakeLastNames...) }
func (f *Faker) Name() string      { return f.FirstName() + " " + f.LastName() }

// Email returns an address at the reserved example.test domain. Addresses from
// one Faker are unique because each carries a per-generator counter.
func (f *Faker) Email() string {
	first, last := strings.ToLower(f.FirstName()), strings.ToLower(f.LastName())
	f.mu.Lock()
	f.emails++
	number := f.emails
	f.mu.Unlock()
	return first + "." + last + "." + strconv.FormatUint(number, 10) + "@example.test"
}

// Word returns one lowercase word; Words returns count words.
func (f *Faker) Word() string { return Pick(f, fakeWords...) }
func (f *Faker) Words(count int) []string {
	result := make([]string, max(count, 0))
	for i := range result {
		result[i] = f.Word()
	}
	return result
}

// Sentence returns a capitalized sentence of four to ten words.
func (f *Faker) Sentence() string {
	text := strings.Join(f.Words(4+f.intN(7)), " ")
	return strings.ToUpper(text[:1]) + text[1:] + "."
}

// IntBetween returns an integer in the inclusive range [low, high].
func (f *Faker) IntBetween(low, high int64) int64 {
	if high < low {
		low, high = high, low
	}
	// Unsigned wraparound keeps the full int64 range exact.
	span := uint64(high-low) + 1
	f.mu.Lock()
	defer f.mu.Unlock()
	if span == 0 {
		return int64(f.random.Uint64())
	}
	return low + int64(f.random.Uint64N(span))
}

// Float returns a value in [0, 1).
func (f *Faker) Float() float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.random.Float64()
}

// Bool returns true or false with equal probability.
func (f *Faker) Bool() bool { return f.intN(2) == 1 }

// TimeBetween returns an instant in [from, to), truncated to microseconds so
// it round-trips through PostgreSQL timestamps.
func (f *Faker) TimeBetween(from, to time.Time) time.Time {
	if !to.After(from) {
		return from.Truncate(time.Microsecond)
	}
	span := to.Sub(from)
	f.mu.Lock()
	offset := time.Duration(f.random.Int64N(int64(span)))
	f.mu.Unlock()
	return from.Add(offset).Truncate(time.Microsecond)
}
