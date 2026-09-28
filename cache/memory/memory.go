// Package memory supplies a bounded, concurrency-safe cache backend for local
// development and deterministic tests. It does not provide distributed guarantees.
package memory

import (
	"container/list"
	"context"
	"crypto/sha256"
	"slices"
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
)

// Config bounds both entry count and the sum of physical-key and payload bytes.
// Map/list overhead is additionally bounded by MaxEntries. LRU eviction applies
// after expired entries have been reclaimed. An oversized entry is rejected.
type Config struct {
	MaxEntries int
	MaxBytes   int
}

func DefaultConfig() Config { return Config{MaxEntries: 10000, MaxBytes: 64 << 20} }
func (c Config) Validate() error {
	if c.MaxEntries <= 0 || c.MaxBytes <= 0 {
		return fault.New(fault.Invalid, "memory cache limits must be positive")
	}
	return nil
}

type entryKind uint8

const (
	plainEntry entryKind = iota
	tagMetadata
	taggedEntry
)

type entry struct {
	kind        entryKind
	fingerprint [sha256.Size]byte
	key         cache.EntryKey
	data        []byte
	expires     time.Time
	persistent  bool
	cost        int
}

// Backend owns immutable serialized snapshots and their expiry/LRU indexes.
// New performs no I/O and starts no goroutines. Close rejects new operations and
// releases this backend's memory; typed Store handles borrow this capability.
type Backend struct {
	mu        sync.Mutex
	config    Config
	clock     clock.Clock
	entries   map[cache.EntryKey]*list.Element
	order     list.List
	bytes     int
	evictions uint64
	// nextExpiry is a conservative lower bound, not a second entry index.
	// Removal may leave it early; a due sweep recomputes it from live entries.
	// hasExpiry distinguishes a finite expiry at time.Time{} from no expiry.
	nextExpiry time.Time
	hasExpiry  bool
	closed     bool
}

var _ cache.Backend = (*Backend)(nil)

func New(config Config, source clock.Clock) (*Backend, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if source == nil {
		return nil, fault.New(fault.Invalid, "memory cache requires a clock")
	}
	return &Backend{config: config, clock: source, entries: make(map[cache.EntryKey]*list.Element)}, nil
}
func (b *Backend) validateContext(ctx context.Context) error {
	if b == nil || b.clock == nil || ctx == nil {
		return fault.New(fault.Invalid, "memory cache operation is not initialized")
	}
	return ctx.Err()
}
func (b *Backend) validate(ctx context.Context, key cache.EntryKey) error {
	if err := b.validateContext(ctx); err != nil {
		return err
	}
	return key.Validate()
}
func (b *Backend) active(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.closed {
		return fault.New(fault.Closed, "memory cache is closed")
	}
	return nil
}
func expired(item *entry, now time.Time) bool {
	return !item.persistent && !now.Before(item.expires)
}
func (b *Backend) remove(element *list.Element) {
	item := element.Value.(*entry)
	delete(b.entries, item.key)
	b.order.Remove(element)
	b.bytes -= item.cost
}
func (b *Backend) lookup(key cache.EntryKey, now time.Time) *list.Element {
	element := b.entries[key]
	if element != nil && expired(element.Value.(*entry), now) {
		b.remove(element)
		return nil
	}
	return element
}
func (b *Backend) Get(ctx context.Context, key cache.EntryKey) ([]byte, bool, error) {
	if err := b.validate(ctx, key); err != nil {
		return nil, false, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.active(ctx); err != nil {
		return nil, false, err
	}
	now := b.clock.Now()
	element := b.lookup(key, now)
	if element == nil {
		return nil, false, nil
	}
	b.order.MoveToFront(element)
	return slices.Clone(element.Value.(*entry).data), true, nil
}
func (b *Backend) Put(ctx context.Context, key cache.EntryKey, data []byte, ttl cache.TTL) error {
	_, err := b.write(ctx, key, data, ttl, false)
	return err
}
func (b *Backend) Add(ctx context.Context, key cache.EntryKey, data []byte, ttl cache.TTL) (bool, error) {
	return b.write(ctx, key, data, ttl, true)
}
func (b *Backend) write(ctx context.Context, key cache.EntryKey, data []byte, ttl cache.TTL, onlyAbsent bool) (bool, error) {
	if err := b.validate(ctx, key); err != nil {
		return false, err
	}
	if err := ttl.Validate(); err != nil {
		return false, err
	}
	cost, err := b.entryCost(key, len(data))
	if err != nil {
		return false, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.active(ctx); err != nil {
		return false, err
	}
	now := b.clock.Now()
	previous := b.lookup(key, now)
	if onlyAbsent && previous != nil {
		return false, nil
	}
	item := makeEntry(key, data, cost, ttl, now)
	b.retain(item, now)
	return true, nil
}

// entryCost validates before adding lengths, including potential integer overflow.
func (b *Backend) entryCost(key cache.EntryKey, valueBytes int) (int, error) {
	keyBytes := len(key.String())
	if keyBytes > b.config.MaxBytes || valueBytes > b.config.MaxBytes-keyBytes {
		return 0, fault.New(fault.Invalid, "entry exceeds memory cache byte budget")
	}
	return keyBytes + valueBytes, nil
}
func makeEntry(key cache.EntryKey, data []byte, cost int, ttl cache.TTL, now time.Time) *entry {
	item := &entry{key: key, data: slices.Clone(data), cost: cost, persistent: ttl.IsForever()}
	if !ttl.IsForever() {
		item.expires = now.Add(ttl.Duration())
	}
	return item
}

// retain owns replacement and eviction for every mutation. Call with b.mu held,
// validated cost and owned data; no operation below can reject a replacement.
func (b *Backend) retain(item *entry, now time.Time) {
	if previous := b.entries[item.key]; previous != nil {
		b.remove(previous)
	}
	if len(b.entries) >= b.config.MaxEntries || item.cost > b.config.MaxBytes-b.bytes {
		b.purgeExpired(now)
	}
	for len(b.entries) >= b.config.MaxEntries || item.cost > b.config.MaxBytes-b.bytes {
		b.remove(b.order.Back())
		b.evictions++
	}
	b.entries[item.key] = b.order.PushFront(item)
	b.bytes += item.cost
	b.trackExpiry(item)
}

func (b *Backend) Forget(ctx context.Context, key cache.EntryKey) (bool, error) {
	if err := b.validate(ctx, key); err != nil {
		return false, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.active(ctx); err != nil {
		return false, err
	}
	now := b.clock.Now()
	element := b.lookup(key, now)
	if element == nil {
		return false, nil
	}
	b.remove(element)
	return true, nil
}
func (b *Backend) trackExpiry(item *entry) {
	if !item.persistent && (!b.hasExpiry || item.expires.Before(b.nextExpiry)) {
		b.nextExpiry, b.hasExpiry = item.expires, true
	}
}

func (b *Backend) purgeExpired(now time.Time) {
	if !b.hasExpiry || now.Before(b.nextExpiry) {
		return
	}
	b.nextExpiry, b.hasExpiry = time.Time{}, false
	for _, element := range b.entries {
		item := element.Value.(*entry)
		if expired(item, now) {
			b.remove(element)
		} else {
			b.trackExpiry(item)
		}
	}
}

// Stats reports live entries after lazy expiry collection. Bytes includes keys
// and payloads; it is not a measurement of total process memory.
type Stats struct {
	Entries   int
	Bytes     int
	Evictions uint64
	Closed    bool
}

func (b *Backend) Stats() Stats {
	if b == nil || b.clock == nil {
		return Stats{Closed: true}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.purgeExpired(b.clock.Now())
	return Stats{Entries: len(b.entries), Bytes: b.bytes, Evictions: b.evictions, Closed: b.closed}
}
func (b *Backend) Close(ctx context.Context) error {
	if b == nil || ctx == nil {
		return fault.New(fault.Invalid, "memory cache close requires a backend and context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	b.closed = true
	b.entries = nil
	b.order.Init()
	b.bytes = 0
	b.nextExpiry, b.hasExpiry = time.Time{}, false
	return nil
}
