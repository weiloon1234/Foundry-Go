package query

import "sync"

// Memo builds one immutable generated declaration per process on first use.
// Its zero value is ready, so generated package variables need no initializer
// and cannot form a static initialization cycle through application hooks or
// relation declarations that call the generated accessor again. Like
// sync.OnceValue, a build that panics panics again on every later call; build
// must not call the same Memo.
type Memo[T any] struct {
	once      sync.Once
	valid     bool
	recovered any
	value     T
}

// Get returns the memoized value, running build exactly once.
func (m *Memo[T]) Get(build func() T) T {
	m.once.Do(func() {
		defer func() {
			if !m.valid {
				m.recovered = recover()
				panic(m.recovered)
			}
		}()
		m.value = build()
		m.valid = true
	})
	if !m.valid {
		panic(m.recovered)
	}
	return m.value
}
