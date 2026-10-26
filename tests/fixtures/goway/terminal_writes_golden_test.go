package goway_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"foundry.test/goway/coremodels"
	n "foundry.test/goway/models/hooks"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/database/prune"
	"strings"
	"testing"
	"time"
)

func TestTerminalWritesGolden(t *testing.T) {
	for _, operation := range []string{"create_each", "create_or_first", "create_or_first_conflict", "lookup_found", "lookup_create", "lookup_update", "prune", "mass_prune", "registry_prune", "registry_mass_prune", "explain", "analyze", "locked_explain", "locked_analyze"} {
		t.Run(operation, func(t *testing.T) {
			var transcripts []string
			{
				now := time.Date(2026, 1, 2, 3, 4, 5, 123456000, time.UTC)
				row := []driver.Value{int64(1), "stored", nil, []byte{1}, now, now, nil}
				columns := []string{"id", "name", "note", "body", "created_at", "updated_at", "deleted_at"}
				g := &goldenCapture{reply: func(sql string, _ []driver.NamedValue) ([]string, [][]driver.Value) {
					if strings.HasPrefix(sql, "EXPLAIN") {
						document := `[{"Plan":{"Node Type":"Seq Scan","Startup Cost":0,"Total Cost":2,"Plan Rows":1,"Plan Width":8}}]`
						if strings.Contains(sql, "ANALYZE TRUE") {
							document = `[{"Plan":{"Node Type":"Seq Scan","Startup Cost":0,"Total Cost":2,"Plan Rows":1,"Plan Width":8,"Actual Rows":1,"Actual Loops":1,"Actual Startup Time":0,"Actual Total Time":0.1},"Execution Time":0.2}]`
						}
						return []string{"QUERY PLAN"}, [][]driver.Value{{document}}
					}
					if strings.Contains(sql, "SELECT EXISTS") {
						return []string{"exists"}, [][]driver.Value{{true}}
					}
					if operation == "lookup_create" && strings.HasPrefix(sql, "SELECT") {
						return columns, [][]driver.Value{}
					}
					return columns, [][]driver.Value{row}
				}}
				if operation == "create_or_first_conflict" {
					violation := errors.New("unique")
					g.queryError = func(s string) error {
						if strings.HasPrefix(s, "INSERT") {
							return violation
						}
						return nil
					}
					g.classify = func(error) database.Detail {
						return database.Detail{Code: database.UniqueViolation, Constraint: "members_pkey", Schema: "fixture", Table: "hook_members"}
					}
				}
				runRegistry := func(ctx context.Context, db database.Transactor, target prune.Target) error {
					registry, err := prune.New(target)
					if err != nil {
						return err
					}
					result, err := registry.Run(ctx, db, prune.Options{BatchSize: 2, MaxBatches: 1})
					if err == nil && (len(result.Counts) != 1 || result.Counts[0].Removed != 1 || result.Counts[0].Remaining) {
						t.Fatal("registry lost prune count", result)
					}
					return err
				}
				mode := prune.Lifecycle
				if operation == "registry_mass_prune" {
					mode = prune.Mass
				}
				check := func(err error) {
					if err != nil {
						t.Fatalf("%v\n%s", err, g.output.String())
					}
				}
				var trace []string
				ctx := coremodels.LifecycleContext(t.Context(), &trace)
				db := g.open(ctx)
				defer db.Close(ctx)

				f := n.Members
				q := f.Query()
				draft := []orm.Assignment[n.Member]{f.Key().Set(1), f.Name.Set(" input "), f.Body.Set([]byte{1})}
				switch operation {
				case "create_each":
					_, err := orm.CreateEach(ctx, db, q, [][]orm.Assignment[n.Member]{draft, draft})
					check(err)
				case "create_or_first", "create_or_first_conflict":
					_, err := orm.CreateOrFirst(ctx, db, q.Where(f.ID.Eq(1)), draft...)
					check(err)
				case "lookup_found", "lookup_create":
					_, err := orm.FirstOrCreate(ctx, db, q.Where(f.ID.Eq(1)), draft...)
					check(err)
				case "lookup_update":
					_, err := orm.UpdateOrCreate(ctx, db, q.Where(f.ID.Eq(1)), draft, func(context.Context, *database.Tx, n.Member) ([]orm.Assignment[n.Member], error) {
						return []orm.Assignment[n.Member]{f.Name.Set(" changed ")}, nil
					})
					check(err)
				case "prune", "mass_prune":
					_, err := orm.ForcePruneBatch(ctx, db, q, f.Soft, 2, operation == "mass_prune")
					check(err)
				case "registry_prune", "registry_mass_prune":
					check(runRegistry(ctx, db, prune.ORMSoftModel("app.members", q, f.Soft, mode)))
				case "explain":
					_, err := orm.Explain(ctx, db, q)
					check(err)
				case "analyze":
					_, err := orm.ExplainAnalyze(ctx, db, q)
					check(err)
				case "locked_explain", "locked_analyze":
					check(db.Transaction(ctx, func(tx *database.Tx) error {
						if operation == "locked_analyze" {
							_, err := orm.ExplainAnalyzeLocked(ctx, tx, q.ForUpdate())
							return err
						}
						_, err := orm.ExplainLocked(ctx, tx, q.ForUpdate())
						return err
					}))
				}

				fmt.Fprintln(&g.output, oracleSnapshot([]any{trace}))
				transcripts = append(transcripts, g.output.String())
			}
			if !oracleEqual(t, "terminal_writes_golden/1", transcripts[0]) {
				t.Fatal("frozen legacy comparison differs")
			}
		})
	}
}
