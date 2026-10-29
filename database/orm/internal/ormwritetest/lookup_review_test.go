package ormwritetest

import "github.com/weiloon1234/Foundry-Go/database/orm"

import (
	"context"
	"database/sql/driver"
	"errors"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/internal/lifecore"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/codec"
	"github.com/weiloon1234/Foundry-Go/database/lifecycle"
)

func TestDraftCallbackErrorsRemainPrivate(t *testing.T) {
	for _, each := range []bool{false, true} {
		cause := &privateLifecycleError{modelValue: "private-draft-model"}
		f := eachFields(orm.Hooks[eachRow]{})
		var statements []string
		db := testDB(t, testConnector{statements: &statements, columns: []string{"id", "name"}, values: [][]driver.Value{{int64(1), cause.modelValue}}})
		draft := func(context.Context, *database.Tx, eachRow) ([]orm.Assignment[eachRow], error) { return nil, cause }
		var err error
		if each {
			_, err = orm.UpdateEach(t.Context(), db, f.Query(), 1, draft)
		} else {
			_, err = orm.UpdateOrCreate(t.Context(), db, f.Query(), nil, draft)
		}
		requireSafeLifecycleError(t, err, cause)
		for _, s := range statements {
			if strings.HasPrefix(s, "UPDATE") {
				t.Fatal("failed draft sent mutation")
			}
		}
	}
}
func TestExplainCompilationErrorIsPrivate(t *testing.T) {
	cause := &privateLifecycleError{modelValue: "private-explain-binding"}
	c := codec.New(func(string) (driver.Value, error) { return nil, cause }, func(v any) (string, error) { return v.(string), nil })
	f := orm.Define("private_explain", func(b orm.Builder[eachRow], m *eachRow) eachTable {
		return eachTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Name, orm.Codec(c))}
	})
	var statements []string
	db := testDB(t, testConnector{statements: &statements})
	_, err := orm.Explain(t.Context(), db, f.Query().Where(orm.TextEq(f.Name, cause.modelValue)))
	requireSafeLifecycleError(t, err, cause)
	if len(statements) != 0 {
		t.Fatal("failed compile sent SQL")
	}
}
func TestCreateOrFirstConflictProvenanceAndSavepoint(t *testing.T) {
	for _, scenario := range []string{"own", "foreign", "foreign_schema", "foreign_table", "missing_schema", "missing_table", "missing_constraint", "invisible", "hook", "after_hook", "hydration", "rollback_failure", "release_failure", "commit_unknown", "created"} {
		t.Run(scenario, func(t *testing.T) {
			violation := errors.New("private-unique-input")
			var classified error
			cleanup := errors.New("private-cleanup-error")
			commits, retrieved := 0, 0
			f := orm.Define("lookup_conflicts", func(b orm.Builder[eachRow], m *eachRow) eachTable {
				c := codec.New(func(v string) (driver.Value, error) { return v, nil }, func(v any) (string, error) {
					if scenario == "hydration" {
						return "", classified
					}
					return v.(string), nil
				})
				return eachTable{orm.TableOf(b, &m.ID), orm.TextOf(b, &m.Name, orm.Codec(c))}
			}, orm.WithHooks(func() orm.Hooks[eachRow] {
				return orm.Hooks[eachRow]{
					Creating: func(context.Context, *database.Tx, *orm.Write[eachRow]) error {
						if scenario == "hook" {
							return classified
						}
						return nil
					},
					Created: func(context.Context, *database.Tx, orm.Changes[eachRow]) error {
						if scenario == "after_hook" {
							return classified
						}
						return nil
					},
					AfterCommit: func(context.Context, lifecycle.Operation, orm.Changes[eachRow]) error { commits++; return nil },
				}
			}), orm.WithRetrievalHooks(func() orm.RetrievalHooks[eachRow] { retrieved++; return orm.RetrievalHooks[eachRow]{} }))
			var statements []string
			c := testConnector{statements: &statements, respondBound: func(s string, args []driver.NamedValue) ([]string, [][]driver.Value, error) {
				if s == "SELECT provenance_error" {
					return nil, nil, violation
				}
				if strings.HasPrefix(s, "INSERT") {
					if scenario == "after_hook" || scenario == "hydration" || scenario == "created" || scenario == "release_failure" {
						return []string{"id", "name"}, [][]driver.Value{{int64(1), "same"}}, nil
					}
					return nil, nil, violation
				}
				if strings.Contains(s, "pg_catalog.pg_index") {
					if !strings.Contains(s, "n.nspname = $3") || !strings.Contains(s, "t.relname = $4") {
						t.Error("missing resolved schema/table validation")
					}
					if len(args) != 4 || args[0].Value != `"lookup_conflicts"` || args[1].Value != "unique_name" {
						t.Error("lost table/constraint provenance", args)
					}
					return []string{"exists"}, [][]driver.Value{{scenario != "foreign" && args[2].Value == "own_schema" && args[3].Value == "lookup_conflicts"}}, nil
				}
				if strings.Contains(s, "SELECT EXISTS") {
					return []string{"exists"}, [][]driver.Value{{true}}, nil
				}
				if scenario == "invisible" {
					return []string{"id", "name"}, nil, nil
				}
				return []string{"id", "name"}, [][]driver.Value{{int64(2), "same"}}, nil
			}, execError: func(s string) error {
				if scenario == "rollback_failure" && strings.HasPrefix(s, "ROLLBACK TO") || scenario == "release_failure" && strings.HasPrefix(s, "RELEASE") {
					return cleanup
				}
				return nil
			}}
			if scenario == "commit_unknown" {
				c.commitErr = cleanup
			}
			db, err := database.Open(t.Context(), database.Adapter{Connector: c, Classify: func(err error) database.Detail {
				if errors.Is(err, violation) {
					detail := database.Detail{Code: database.UniqueViolation, Constraint: "unique_name", Schema: "own_schema", Table: "lookup_conflicts"}
					switch scenario {
					case "foreign_schema":
						detail.Schema = "other_schema"
					case "foreign_table":
						detail.Table = "other_table"
					case "missing_schema":
						detail.Schema = ""
					case "missing_table":
						detail.Table = ""
					case "missing_constraint":
						detail.Constraint = ""
					}
					return detail
				}
				return database.Detail{Code: database.QueryFailed}
			}}, database.DefaultPoolConfig())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close(context.Background())
			_, classified = db.Query(t.Context(), "SELECT provenance_error")
			if classified == nil {
				t.Fatal("missing native constraint error")
			}
			statements = nil
			got, err := orm.CreateOrFirst(t.Context(), db, f.Query().Where(orm.TextEq(f.Name, "same")), orm.KeySet(f.Key(), 1))
			if scenario == "own" || scenario == "created" {
				if err != nil || got.Name != "same" {
					t.Fatal(got, err)
				}
			} else if err == nil || got.ID != 0 {
				t.Fatal("failure published model", got, err)
			}
			if scenario == "commit_unknown" {
				var outcome *database.Error
				if !errors.As(err, &outcome) || outcome.Outcome() != database.Unknown {
					t.Fatal("unknown commit reported as definite", err)
				}
				var pending *orm.WriteError[eachRow]
				if !errors.As(err, &pending) || pending.Candidate().ID != 2 {
					t.Fatal("lost reconciliation candidate", err)
				}
			}
			inserts, probes, reads, rollback := 0, 0, 0, false
			for _, s := range statements {
				if strings.HasPrefix(s, "INSERT") {
					inserts++
				}
				if strings.HasPrefix(s, "ROLLBACK TO") {
					rollback = true
				}
				if strings.Contains(s, "pg_catalog.pg_index") {
					probes++
					if !rollback {
						t.Fatal("provenance checked before rollback")
					}
				}
				if strings.HasPrefix(s, `SELECT "lookup_conflicts"`) {
					reads++
					if !rollback {
						t.Fatal("competitor read before rollback")
					}
				}
			}
			if inserts > 1 || retrieved != 0 {
				t.Fatal("retried or dispatched retrieval", statements)
			}
			if scenario == "hook" || scenario == "after_hook" || scenario == "hydration" || scenario == "rollback_failure" || scenario == "release_failure" || strings.HasPrefix(scenario, "missing_") {
				if probes != 0 || reads != 0 {
					t.Fatal("unproven conflict resolved", statements)
				}
			}
			if strings.HasPrefix(scenario, "foreign") && reads != 0 {
				t.Fatal("foreign conflict read competitor")
			}
			wantCommits := 0
			if scenario == "created" {
				wantCommits = 1
			}
			if commits != wantCommits {
				t.Fatal("savepoint callbacks escaped", commits)
			}
		})
	}
}

func TestLookupUpdateCallbackUsesSharedDepthLimit(t *testing.T) {
	ctx := t.Context()
	for range lifecore.MaxDepth {
		var err error
		ctx, err = lifecore.Context(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	f := eachFields(orm.Hooks[eachRow]{})
	var statements []string
	db := testDB(t, testConnector{statements: &statements, columns: []string{"id", "name"}, values: [][]driver.Value{{int64(1), "existing"}}})
	calls := 0
	_, err := orm.UpdateOrCreate(ctx, db, f.Query(), nil, func(context.Context, *database.Tx, eachRow) ([]orm.Assignment[eachRow], error) {
		calls++
		return nil, nil
	})
	if !errors.Is(err, fault.Invalid) || calls != 0 {
		t.Fatal("lookup callback escaped lifecycle depth", err, calls)
	}
	for _, s := range statements {
		if strings.HasPrefix(s, "UPDATE") || strings.HasPrefix(s, "INSERT") {
			t.Fatal("depth failure executed mutation", s)
		}
	}
}
