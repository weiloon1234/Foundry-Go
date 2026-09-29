// Package memory supplies a bounded, concurrency-safe cache backend for local
// development and deterministic tests. It does not provide distributed guarantees.
package memory

import (
	"container/heap"
	"container/list"
	"context"
	"crypto/sha256"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/weiloon1234/Foundry-Go/cache"
	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/frameworkadapter"
)

// Config bounds both entry count and the sum of physical-key and payload bytes.
// Map/list overhead is additionally bounded by MaxEntries. Recency eviction
// (LRU with a second chance for entries read since they were last ordered)
// applies after expired entries have been reclaimed. An oversized entry is rejected.
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

// entry data is immutable once retained; replacements create a new entry, so
// readers may copy a captured slice after releasing the lock.
type entry struct {
	kind        entryKind
	fingerprint [sha256.Size]byte
	key         cache.EntryKey
	data        []byte
	expires     time.Time
	persistent  bool
	cost        int
	// expiryIndex is this entry's position in Backend.expiry, or -1.
	expiryIndex int
	// referenced records a read under the shared lock; eviction gives such an
	// entry a second chance (moves it to the front) instead of removing it.
	referenced atomic.Bool
}

// expiryHeap orders finite-expiry entries by deadline so reclamation pops only
// expired entries (O(log n) each) instead of sweeping the whole map.
type expiryHeap []*entry

func (h expiryHeap) Len() int           { return len(h) }
func (h expiryHeap) Less(i, j int) bool { return h[i].expires.Before(h[j].expires) }
func (h expiryHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].expiryIndex, h[j].expiryIndex = i, j
}
func (h *expiryHeap) Push(value any) {
	item := value.(*entry)
	item.expiryIndex = len(*h)
	*h = append(*h, item)
}
func (h *expiryHeap) Pop() any {
	old := *h
	item := old[len(old)-1]
	old[len(old)-1] = nil
	item.expiryIndex = -1
	*h = old[:len(old)-1]
	return item
}

// Backend owns immutable serialized snapshots and their expiry/LRU indexes.
// New performs no I/O and starts no goroutines. Close rejects new operations and
// releases this backend's memory; typed Store handles borrow this capability.
// Reads share a read lock and never reorder storage: they mark entries
// referenced, and expired or unusable entries they meet are reclaimed later by
// writes, capacity pressure or Stats. Mutations take the exclusive lock.
type Backend struct {
	mu        sync.RWMutex
	config    Config
	clock     clock.Clock
	entries   map[cache.EntryKey]*list.Element
	order     list.List
	bytes     int
	evictions uint64
	expiry    expiryHeap
	closed    bool
}

var _ cache.Backend = (*Backend)(nil)

// FoundryAdapter marks the backend as framework-owned adapter I/O.
func (*Backend) FoundryAdapter(frameworkadapter.Seal) {}

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
	if item.expiryIndex >= 0 {
		heap.Remove(&b.expiry, item.expiryIndex)
	}
}
func (b *Backend) lookup(key cache.EntryKey, now time.Time) *list.Element {
	element := b.entries[key]
	if element != nil && expired(element.Value.(*entry), now) {
		b.remove(element)
		return nil
	}
	return element
}

// live returns an unexpired element without mutating storage; it is safe under
// the read lock.
func (b *Backend) live(key cache.EntryKey, now time.Time) *list.Element {
	element := b.entries[key]
	if element == nil || expired(element.Value.(*entry), now) {
		return nil
	}
	return element
}

// touch records a read for second-chance eviction; safe under the read lock.
func touch(element *list.Element) { element.Value.(*entry).referenced.Store(true) }

// reclaim removes an entry that a shared-lock read found expired, or obsolete
// for its address, if the same retained entry is still stored.
func (b *Backend) reclaim(keys []cache.EntryKey, elements []*list.Element, obsolete bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	now := b.clock.Now()
	for i, element := range elements {
		if b.entries[keys[i]] == element && (obsolete || expired(element.Value.(*entry), now)) {
			b.remove(element)
		}
	}
}

func (b *Backend) Get(ctx context.Context, key cache.EntryKey) ([]byte, bool, error) {
	if err := b.validate(ctx, key); err != nil {
		return nil, false, err
	}
	b.mu.RLock()
	if err := b.active(ctx); err != nil {
		b.mu.RUnlock()
		return nil, false, err
	}
	element := b.entries[key]
	if element == nil {
		b.mu.RUnlock()
		return nil, false, nil
	}
	if expired(element.Value.(*entry), b.clock.Now()) {
		b.mu.RUnlock()
		b.reclaim([]cache.EntryKey{key}, []*list.Element{element}, false)
		return nil, false, nil
	}
	touch(element)
	data := element.Value.(*entry).data
	b.mu.RUnlock()
	// Retained data is immutable, so the owned copy is made outside the lock.
	return slices.Clone(data), true, nil
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
	item := &entry{key: key, data: slices.Clone(data), cost: cost, persistent: ttl.IsForever(), expiryIndex: -1}
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
	// Reads cannot set flags while the exclusive lock is held, so every pass
	// either clears one flag or evicts: the loop ends within 2*entries steps.
	for len(b.entries) >= b.config.MaxEntries || item.cost > b.config.MaxBytes-b.bytes {
		back := b.order.Back()
		if back.Value.(*entry).referenced.Swap(false) {
			b.order.MoveToFront(back)
			continue
		}
		b.remove(back)
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

// trackExpiry (re)indexes a retained entry after its expiry was set or changed.
func (b *Backend) trackExpiry(item *entry) {
	switch {
	case item.persistent && item.expiryIndex >= 0:
		heap.Remove(&b.expiry, item.expiryIndex)
	case item.persistent:
	case item.expiryIndex >= 0:
		heap.Fix(&b.expiry, item.expiryIndex)
	default:
		heap.Push(&b.expiry, item)
	}
}

// purgeExpired reclaims only entries whose deadline has passed, earliest first.
func (b *Backend) purgeExpired(now time.Time) {
	for len(b.expiry) > 0 && expired(b.expiry[0], now) {
		b.remove(b.entries[b.expiry[0].key])
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
	b.expiry = nil
	return nil
}
