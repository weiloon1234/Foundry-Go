package cache

import (
	"context"
	"slices"
	"strings"
	"sync"
)

// Memo bounds: a full memo stops recording; reads then use the Store as usual.
const (
	MaxMemoEntries = 256
	MaxMemoBytes   = 1 << 20
)

type memoContextKey struct{}

// memo holds owned copies of stored payloads read through one context.
type memo struct {
	mu      sync.Mutex
	entries map[memoKey]memoEntry
	bytes   int
}

// memoKey separates stores and tag views; tags is the view's canonical tag
// identity ("" without application tags).
type memoKey struct {
	store *Store
	base  EntryKey
	tags  string
}
type memoEntry struct {
	data  []byte
	found bool
}

// WithMemo returns a context that memoizes typed cache reads for its lifetime,
// typically one request. Within it, the first Get, GetMany, Exists or Remember
// of a key reads the Store; later reads of that key through the same context
// (and its children) return the memoized result without I/O, even if another
// request or process has since changed the entry. Writes (Put, PutMany, Add,
// Forget, ForgetMany, Increment, Expire) through the context forget the key's
// memoized results, and Store.Invalidate/InvalidateTags forget the whole
// store's. At most MaxMemoEntries results and MaxMemoBytes of payload are
// memoized; later reads are not recorded. A context that already memoizes is
// returned unchanged. Each result still decodes its own copy.
func WithMemo(ctx context.Context) context.Context {
	if ctx == nil || currentMemo(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, memoContextKey{}, &memo{entries: make(map[memoKey]memoEntry)})
}

func currentMemo(ctx context.Context) *memo {
	m, _ := ctx.Value(memoContextKey{}).(*memo)
	return m
}

// memoView is one handle's memo scope for a call; its zero value memoizes nothing.
type memoView struct {
	memo  *memo
	store *Store
	tags  string
}

// memoView resolves the memo and this view's tag identity once per call.
func (c Cache[K, V]) memoView(ctx context.Context) (memoView, error) {
	m := currentMemo(ctx)
	if m == nil {
		return memoView{}, nil
	}
	view := memoView{memo: m, store: c.store}
	if len(c.tags) != 0 {
		keys, err := resolveTagKeys(c.tags)
		if err != nil {
			return memoView{}, err
		}
		texts := make([]string, len(keys))
		for i, key := range keys {
			texts[i] = key.String()
		}
		view.tags = strings.Join(texts, "\x00")
	}
	return view, nil
}
func (v memoView) load(base EntryKey) (memoEntry, bool) {
	if v.memo == nil {
		return memoEntry{}, false
	}
	v.memo.mu.Lock()
	defer v.memo.mu.Unlock()
	entry, ok := v.memo.entries[memoKey{v.store, base, v.tags}]
	return entry, ok
}

// save records an owned copy of data; a full memo records nothing.
func (v memoView) save(base EntryKey, data []byte, found bool) {
	if v.memo == nil {
		return
	}
	v.memo.mu.Lock()
	defer v.memo.mu.Unlock()
	key := memoKey{v.store, base, v.tags}
	if previous, ok := v.memo.entries[key]; ok {
		v.memo.bytes -= len(previous.data)
		delete(v.memo.entries, key)
	}
	if len(v.memo.entries) >= MaxMemoEntries || len(data) > MaxMemoBytes-v.memo.bytes {
		return
	}
	entry := memoEntry{found: found}
	if found {
		entry.data = slices.Clone(data)
		if entry.data == nil {
			entry.data = []byte{}
		}
	}
	v.memo.entries[key] = entry
	v.memo.bytes += len(entry.data)
}

// forgetMemo drops every memoized view of base in ctx's memo for store.
func forgetMemo(ctx context.Context, store *Store, bases ...EntryKey) {
	m := currentMemo(ctx)
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, entry := range m.entries {
		if key.store == store && (bases == nil || slices.Contains(bases, key.base)) {
			m.bytes -= len(entry.data)
			delete(m.entries, key)
		}
	}
}
