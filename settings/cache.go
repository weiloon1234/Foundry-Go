package settings

import (
	"sync"
	"time"

	"github.com/weiloon1234/Foundry-Go/clock"
	"github.com/weiloon1234/Foundry-Go/value"
)

// cache retains validated records for keys registered with Options.Cache. It
// is owned by one Manager: there is no process-global state. Each key has an
// epoch; invalidation advances it so a read that started before a write cannot
// install the value it observed afterwards. Records are immutable snapshots, and
// callers always decode fresh values from them.
type cache struct {
	mu      sync.Mutex
	clock   clock.Clock
	entries map[Name]cached
	epochs  map[Name]uint64
}
type cached struct {
	record  value.Optional[Record]
	expires time.Time
}

func newCache(source clock.Clock) *cache {
	return &cache{clock: source, entries: make(map[Name]cached), epochs: make(map[Name]uint64)}
}

// lookup returns a fresh entry and, on a miss, the epoch a subsequent read must
// present to install its result.
func (c *cache) lookup(name Name) (value.Optional[Record], bool, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[name]
	if ok && c.clock.Now().Before(entry.expires) {
		return entry.record, true, 0
	}
	if ok {
		delete(c.entries, name)
	}
	return value.Optional[Record]{}, false, c.epochs[name]
}
func (c *cache) remember(name Name, epoch uint64, ttl time.Duration, record value.Optional[Record]) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.epochs[name] != epoch {
		return
	}
	c.entries[name] = cached{record: record, expires: c.clock.Now().Add(ttl)}
}
func (c *cache) invalidate(names ...Name) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, name := range names {
		c.epochs[name]++
		delete(c.entries, name)
	}
}

// cached reports a hit only for cache-enabled keys.
func (m *Manager) cached(name Name) (value.Optional[Record], bool, uint64) {
	if m.cache == nil || m.keys[name].cache <= 0 {
		return value.Optional[Record]{}, false, 0
	}
	return m.cache.lookup(name)
}
func (m *Manager) remember(name Name, epoch uint64, record value.Optional[Record]) {
	if ttl := m.keys[name].cache; m.cache != nil && ttl > 0 {
		m.cache.remember(name, epoch, ttl, record)
	}
}

// Invalidate drops cached reads for the given keys in this manager. Writes made
// through the manager do this automatically; call it after changing a setting
// by other means, such as a migration or another process's notification.
func (m *Manager) Invalidate(keys ...Selection) error {
	if err := m.Validate(); err != nil {
		return err
	}
	names := make([]Name, 0, len(keys))
	for _, key := range keys {
		registration, err := m.selected(key)
		if err != nil {
			return err
		}
		names = append(names, registration.name)
	}
	m.invalidate(names...)
	return nil
}
func (m *Manager) invalidate(names ...Name) {
	if m.cache != nil {
		m.cache.invalidate(names...)
	}
}
