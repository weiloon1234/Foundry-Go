package reporting_test

import (
	"context"
	"testing"

	"foundry.test/consumer/reporting"
	"github.com/weiloon1234/Foundry-Go/auth"
	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/datatable"
	"github.com/weiloon1234/Foundry-Go/i18n"
	"github.com/weiloon1234/Foundry-Go/model"
	"github.com/weiloon1234/Foundry-Go/secret"
	authtest "github.com/weiloon1234/Foundry-Go/testkit/auth"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"github.com/weiloon1234/Foundry-Go/value"
)

type fixture struct {
	db       *database.DB
	schema   string
	manager  *datatable.Manager
	tempDir  string
	provider reporting.OperatorProvider
	registry *auth.Registry
	guard    auth.Guard[reporting.Operator]
	operator model.ID[reporting.Operator]
	members  []model.ID[reporting.Member]
}

func (f *fixture) transaction(ctx context.Context, fn func(*database.Tx) error) error {
	return f.db.Transaction(ctx, func(tx *database.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL search_path TO "`+f.schema+`", pg_temp`); err != nil {
			return err
		}
		return fn(tx)
	})
}

func openFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{db: pgtest.Open(t), tempDir: t.TempDir()}
	f.schema = pgtest.Namespace(t, f.db)
	var err error
	f.operator, err = model.ParseID[reporting.Operator]("0193fd8c-2075-7000-8000-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"0193fd8c-2075-7000-8000-000000000011", "0193fd8c-2075-7000-8000-000000000012", "0193fd8c-2075-7000-8000-000000000013", "0193fd8c-2075-7000-8000-000000000014", "0193fd8c-2075-7000-8000-000000000015"} {
		id, err := model.ParseID[reporting.Member](text)
		if err != nil {
			t.Fatal(err)
		}
		f.members = append(f.members, id)
	}
	if err := f.transaction(t.Context(), func(tx *database.Tx) error {
		for _, statement := range []string{
			`CREATE TABLE report_operators (id uuid PRIMARY KEY, tenant_id bigint NOT NULL, active boolean NOT NULL, can_view boolean NOT NULL, can_export boolean NOT NULL)`,
			`CREATE TABLE report_members (id uuid PRIMARY KEY, tenant_id bigint NOT NULL, name text NOT NULL, nickname text, state text NOT NULL, balance numeric NOT NULL, deleted_at timestamptz, UNIQUE(id,tenant_id))`,
			`CREATE TABLE report_orders (id uuid PRIMARY KEY, tenant_id bigint NOT NULL, member_id uuid NOT NULL, label text NOT NULL, amount numeric NOT NULL, deleted_at timestamptz, FOREIGN KEY(member_id,tenant_id) REFERENCES report_members(id,tenant_id))`,
			`INSERT INTO report_operators VALUES ('0193fd8c-2075-7000-8000-000000000001',7,true,true,true)`,
			`INSERT INTO report_members VALUES ('0193fd8c-2075-7000-8000-000000000011',7,' Ada ',NULL,'active',1.25,NULL),('0193fd8c-2075-7000-8000-000000000012',7,'Ada','second','active',2.5,NULL),('0193fd8c-2075-7000-8000-000000000013',7,'=SUM(A1:A2)','x,y','disabled',9007199254740993.125,NULL),('0193fd8c-2075-7000-8000-000000000014',7,'Deleted',NULL,'active',100,now()),('0193fd8c-2075-7000-8000-000000000015',8,'Foreign',NULL,'active',200,NULL)`,
			`INSERT INTO report_orders VALUES ('0193fd8c-2075-7000-8000-000000000021',7,'0193fd8c-2075-7000-8000-000000000011','priority',10,NULL),('0193fd8c-2075-7000-8000-000000000022',7,'0193fd8c-2075-7000-8000-000000000011','regular',20,NULL),('0193fd8c-2075-7000-8000-000000000023',7,'0193fd8c-2075-7000-8000-000000000012','priority',5,NULL),('0193fd8c-2075-7000-8000-000000000024',7,'0193fd8c-2075-7000-8000-000000000014','hidden',100,NULL),('0193fd8c-2075-7000-8000-000000000025',7,'0193fd8c-2075-7000-8000-000000000011','trashed',999,now()),('0193fd8c-2075-7000-8000-000000000026',8,'0193fd8c-2075-7000-8000-000000000015','foreign',500,NULL)`,
		} {
			if _, err := tx.Exec(t.Context(), statement); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.provider = reporting.Operators(func(ctx context.Context, id model.ID[reporting.Operator]) (value.Optional[reporting.Operator], error) {
		var found value.Optional[reporting.Operator]
		err := f.transaction(ctx, func(tx *database.Tx) error {
			var err error
			found, err = reporting.QueryReportOperators().Find(ctx, tx, id)
			return err
		})
		return found, err
	})
	proof, err := auth.NewProof((reporting.Operator{ID: f.operator}).FoundryReference(), auth.Authenticated)
	if err != nil {
		t.Fatal(err)
	}
	strategy := auth.DefineStrategy("report.test", func(_ context.Context, credential secret.String) (value.Optional[auth.Proof[reporting.Operator, model.ID[reporting.Operator]]], error) {
		if credential.Reveal() != "report-fixture" {
			return value.Optional[auth.Proof[reporting.Operator, model.ID[reporting.Operator]]]{}, nil
		}
		return value.Set(proof), nil
	})
	f.guard = auth.DefineGuard("report.web", f.provider, strategy)
	f.registry, err = auth.NewRegistry(auth.DefaultConfig(), f.guard.Registration(), reporting.Access.Registration())
	if err != nil {
		t.Fatal(err)
	}
	locales, err := i18n.NewLocaleSet("en", "en", "ms")
	if err != nil {
		t.Fatal(err)
	}
	config := datatable.DefaultConfig()
	config.Schema = f.schema
	config.TempDir = f.tempDir
	f.manager, err = reporting.New(datatable.Dependencies{Database: f.db, Locales: locales, Labels: func(_ context.Context, locale i18n.LocaleID, key i18n.MessageKey) (string, error) {
		return string(locale) + ":" + string(key), nil
	}}, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.manager.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return f
}

func (f *fixture) scope(t *testing.T) *auth.Scope {
	t.Helper()
	return authtest.Scope(t, f.registry, auth.Credential{Name: "report.test", Secret: secret.New("report-fixture")})
}
