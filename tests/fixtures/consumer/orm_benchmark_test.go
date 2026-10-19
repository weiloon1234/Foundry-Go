package consumer_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"foundry.test/consumer/internal/queryfixture"
	"foundry.test/consumer/models"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/postgres"
	"github.com/weiloon1234/Foundry-Go/database/query"
	"github.com/weiloon1234/Foundry-Go/temporal"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
)

// Benchmarks use existing generated models. Direct SQL is compiled by Foundry
// before timing, then uses the same adapter and codecs as the generated decoder.
// This isolates query compilation and runtime ownership from database/codec cost.
func ormBenchmarkStore(t testing.TB) (*database.DB, *sql.DB) {
	t.Helper()
	setup := pgtest.Open(t)
	namespace := pgtest.Namespace(t, setup)
	config := pgtest.Config(t)
	config.Schema = namespace
	config.Pool.MaxOpen = 1
	config.Pool.MaxIdle = 1
	adapter, err := postgres.New(config)
	if err != nil {
		t.Fatal(err)
	}
	raw := sql.OpenDB(adapter.Connector)
	raw.SetMaxOpenConns(1)
	raw.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = raw.Close() })
	db, err := database.Open(t.Context(), adapter, config.Pool)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	for _, statement := range []string{
		`CREATE TABLE users (id uuid PRIMARY KEY, email_address text NOT NULL, age bigint NOT NULL, nickname text, status text NOT NULL, level smallint NOT NULL, birthday date, introducer_id uuid)`,
		`INSERT INTO users SELECT md5(i::text)::uuid, 'user-' || i || '@example.test', i, CASE WHEN i % 2 = 0 THEN 'nickname' END, 'active', 1, CASE WHEN i % 2 = 0 THEN DATE '2000-02-29' END, NULL FROM generate_series(1,1000) AS i`,
		`CREATE INDEX users_age_idx ON users(age)`,
		`ANALYZE users`,
	} {
		if _, err := db.Exec(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	return db, raw
}

func ormBenchmarkQuery(size int) models.UserQuery {
	f := models.UserFields()
	return models.QueryUsers().Where(f.Age.Gte(1), f.Status.Eq(models.StatusActive)).OrderBy(f.Age.Asc()).Limit(size)
}

// Keep concrete scanners identical to the persisted types in the declaration.
// The agreement test detects drift from the generated decoder.
var ormBenchmarkScanUser = func() func(database.Row) (models.User, error) {
	id, email, age := codec.ID[models.User](), codec.String[string](), codec.Signed[int]()
	nickname := codec.Nullable(codec.String[string]())
	status := codec.String[models.Status]().Validated(models.Status.Validate)
	level := codec.Unsigned[models.Level]().Validated(models.Level.Validate)
	birthday, introducer := codec.Nullable(codec.Date()), codec.Nullable(codec.ID[models.User]())
	return func(row database.Row) (models.User, error) {
		var item models.User
		err := row.Scan(id.Scan(&item.ID), email.Scan(&item.Email), age.Scan(&item.Age), nickname.Scan(&item.Nickname), status.Scan(&item.Status), level.Scan(&item.Level), birthday.Scan(&item.Birthday), introducer.Scan(&item.IntroducerID))
		if err != nil {
			return models.User{}, err
		}
		return item, nil
	}
}()

func ormBenchmarkRaw(ctx context.Context, raw *sql.DB, statement query.Statement) (items []models.User, err error) {
	rows, err := raw.QueryContext(ctx, statement.SQL(), statement.Arguments()...)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	items = make([]models.User, 0)
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		item, err := ormBenchmarkScanUser(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func ormBenchmarkFrameworkSQL(ctx context.Context, db *database.DB, statement query.Statement) ([]models.User, error) {
	items := make([]models.User, 0)
	err := database.ForEach(ctx, db, statement.SQL(), statement.Arguments(), ormBenchmarkScanUser, func(item models.User) error {
		items = append(items, item)
		return nil
	})
	return items, err
}

func TestORMBenchmarkReadsAgree(t *testing.T) {
	db, raw := ormBenchmarkStore(t)
	for _, size := range []int{1, 100, 1000} {
		q := ormBenchmarkQuery(size)
		statement, err := q.Compile()
		if err != nil {
			t.Fatal(err)
		}
		counter := &queryfixture.QueryCounter{Executor: db}
		want, err := q.All(t.Context(), counter)
		if err != nil || len(want) != size || counter.Queries.Load() != 1 {
			t.Fatal("generated benchmark read failed", err)
		}
		for name, read := range map[string]func() ([]models.User, error){
			"direct":        func() ([]models.User, error) { return ormBenchmarkRaw(t.Context(), raw, statement) },
			"framework-sql": func() ([]models.User, error) { return ormBenchmarkFrameworkSQL(t.Context(), db, statement) },
		} {
			got, err := read()
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("%s benchmark decoder differs from generated models: %v", name, err)
			}
		}
	}
}

func BenchmarkORMPostgresReads(b *testing.B) {
	db, raw := ormBenchmarkStore(b)
	for _, size := range []int{1, 100, 1000} {
		q := ormBenchmarkQuery(size)
		statement, err := q.Compile()
		if err != nil {
			b.Fatal(err)
		}
		for _, variant := range []struct {
			name string
			read func() ([]models.User, error)
		}{
			{"direct-sql", func() ([]models.User, error) { return ormBenchmarkRaw(b.Context(), raw, statement) }},
			{"framework-sql", func() ([]models.User, error) { return ormBenchmarkFrameworkSQL(b.Context(), db, statement) }},
			{"typed-orm", func() ([]models.User, error) { return ormBenchmarkQuery(size).All(b.Context(), db) }},
		} {
			b.Run(fmt.Sprintf("%d/%s", size, variant.name), func(b *testing.B) {
				// Warm both PostgreSQL buffers and the driver's statement cache.
				if items, err := variant.read(); err != nil || len(items) != size {
					b.Fatal("benchmark warm read failed", err)
				}
				b.ReportAllocs()
				for b.Loop() {
					items, err := variant.read()
					if err != nil || len(items) != size {
						b.Fatal("benchmark read failed", err)
					}
				}
				b.ReportMetric(float64(size), "rows/op")
			})
		}
	}
}

func BenchmarkORMCompile(b *testing.B) {
	f := models.UserFields()
	date, err := temporal.ParseDate("2000-01-01")
	if err != nil {
		b.Fatal(err)
	}
	for _, variant := range []struct {
		name  string
		build func() models.UserQuery
	}{
		{"list", func() models.UserQuery { return ormBenchmarkQuery(100) }},
		{"filters", func() models.UserQuery {
			return models.QueryUsers().Where(query.Or(f.Email.Contains("_%!'"), f.Age.In(21, 35, 42)), f.Nickname.IsNotNull(), f.Birthday.Gte(date)).OrderBy(f.Age.Desc(), f.ID.Asc()).Limit(100).Offset(20)
		}},
	} {
		b.Run(variant.name, func(b *testing.B) {
			if _, err := variant.build().Compile(); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := variant.build().Compile(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
