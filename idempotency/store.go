package idempotency

import (
	"context"
	"fmt"
	"strconv"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/internal/sqlscope"
	"github.com/weiloon1234/Foundry-Go/internal/workscope"
)

// Store borrows an existing primary database and owns bounded operation lifetimes.
// No background task, migration, connection or pruning starts in New.
type Store struct {
	db        *database.DB
	namespace Namespace
	config    Config
	calls     *workscope.Group
}

func New(db *database.DB, namespace Namespace, config Config) (*Store, error) {
	if db == nil || namespace.Validate() != nil {
		return nil, invalid("idempotency requires a database and application namespace")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	calls, err := workscope.New(config.MaxActive, config.Timeout)
	if err != nil {
		return nil, err
	}
	return &Store{db: db, namespace: namespace, config: config, calls: calls}, nil
}
func (s *Store) Validate() error {
	if s == nil || s.db == nil || s.calls == nil {
		return invalid("idempotency store is undefined")
	}
	return nil
}
func (s *Store) Database() *database.DB {
	if s == nil {
		return nil
	}
	return s.db
}
func (s *Store) Config() Config {
	if s == nil {
		return Config{}
	}
	return s.config
}
func (s *Store) Close(ctx context.Context) error {
	if err := s.Validate(); err != nil {
		return err
	}
	return s.calls.Close(ctx)
}
func (s *Store) Done() <-chan struct{} {
	if s == nil {
		var calls *workscope.Group
		return calls.Done()
	}
	return s.calls.Done()
}
func (*Store) Format(s fmt.State, _ rune) { _, _ = s.Write([]byte("idempotency store")) }
func (s *Store) scoped(ctx context.Context, tx *database.Tx, fn func(*database.Tx) error) error {
	return sqlscope.InSchema(ctx, tx, s.db, s.config.Schema, fn)
}
func (s *Store) limits(ctx context.Context, tx *database.Tx) error {
	// Statement work and lock waits have independent server bounds. The context
	// also bounds BEGIN, connection acquisition, callbacks and outer commit.
	_, err := tx.Exec(ctx, `SELECT pg_catalog.set_config('statement_timeout', $1, true), pg_catalog.set_config('lock_timeout', $2, true)`, strconv.FormatInt(s.config.Timeout.Milliseconds(), 10), strconv.FormatInt(s.config.DuplicateWait.Milliseconds(), 10))
	return err
}

func (s *Store) namespaceDigest() string {
	return digest("foundry.idempotency.namespace.v1", s.namespace.Application, s.namespace.Environment)
}
