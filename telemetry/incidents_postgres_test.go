package telemetry

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/weiloon1234/Foundry-Go/database"
	"github.com/weiloon1234/Foundry-Go/database/orm"
	"github.com/weiloon1234/Foundry-Go/fault"
	"github.com/weiloon1234/Foundry-Go/http"
	"github.com/weiloon1234/Foundry-Go/observability"
	"github.com/weiloon1234/Foundry-Go/value"
)

func incidentConfig(rules ...Rule) Config {
	config := engineConfig()
	config.Alerts = alertPolicy()
	config.Alerts.Rules = rules
	config.Alerts.SustainCaptures, config.Alerts.RecoverCaptures = 2, 2
	config.Alerts.Thresholds.MinimumRequests, config.Alerts.Thresholds.AuthFailures = 2, 2
	return config
}

func captureIncident(t *testing.T, c *Collector, pool, writeFailures, logDrops float64) batch {
	t.Helper()
	recorder, err := observability.New(observability.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.RegisterCollector("incident.test", func(w *observability.MetricWriter) {
		w.Counter("foundry_log_write_failures_total", "Failed writes.", writeFailures, observability.Label{Name: "channel", Value: "app"})
		w.Counter("foundry_log_dropped_total", "Dropped asynchronous records.", logDrops, observability.Label{Name: "channel", Value: "app"})
	}); err != nil {
		t.Fatal(err)
	}
	if err := c.captureRuntime(recorder, Health{}, pool); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	return c.pending[len(c.pending)-1]
}

func storedIncident(t *testing.T, db *database.DB, schema string, c *Collector, rule Rule) value.Optional[alertRow] {
	t.Helper()
	var result value.Optional[alertRow]
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		var err error
		result, err = orm.First(t.Context(), tx, alerts.Query().Where(orm.KeyEq(alerts.Key(), AlertKey(digest(c.runID.String()+"|"+string(rule))))))
		return err
	})
	return result
}

func requireIncident(t *testing.T, db *database.DB, schema string, c *Collector, rule Rule, state AlertState, episode Episode) alertRow {
	t.Helper()
	row, found := storedIncident(t, db, schema, c, rule).Get()
	if !found || row.State != state || row.Episode != episode {
		t.Fatalf("incident %s = present:%v state:%s episode:%d, want %s/%d", rule, found, row.State, row.Episode, state, episode)
	}
	return row
}

func observeIncidentTraffic(t *testing.T, c *Collector, route http.RouteID, count, status int, duration time.Duration) {
	t.Helper()
	for range count {
		c.ObserveRequest(t.Context(), http.RequestEvent{Route: route, Method: http.GET, Duration: duration, Result: observability.Result{Status: status}})
	}
}

func TestPostgresTelemetryAllIncidentRulesSustainAndRecoverCaptures(t *testing.T) {
	for _, rule := range RuleValues() {
		t.Run(string(rule), func(t *testing.T) {
			s, db, schema, wall := postgresStorage(t, collectorConfig(), incidentConfig(rule))
			c := s.collector
			writeFailures := float64(0)
			captureSignal := func(bad bool, first bool) {
				t.Helper()
				pool := float64(0)
				switch rule {
				case RuleLatency, RuleHTTPErrors:
					if first {
						status, duration := 200, time.Millisecond
						if bad {
							status, duration = 500, 2*time.Second
						}
						observeIncidentTraffic(t, c, "business.read", 2, status, duration)
					}
				case RulePoolPressure:
					if bad {
						pool = .95
					}
				case RuleLogFailures:
					if bad {
						writeFailures++
					}
				case RuleTelemetryLoss:
					if bad {
						c.drop(1)
					}
				case RuleAuthenticationBurst:
					if bad {
						for _, status := range []int{401, 429} {
							c.ObserveSecurityRequest(t.Context(), http.SecurityRequestEvent{Request: http.RequestEvent{Result: observability.Result{Status: status}}})
						}
					}
				}
				captureIncident(t, c, pool, writeFailures, 100)
				if err := s.flush(t.Context(), nil); err != nil {
					t.Fatal(err)
				}
			}
			captureSignal(true, true)
			if storedIncident(t, db, schema, c, rule).IsSet() {
				t.Fatal("one bad capture satisfied a two-capture sustain")
			}
			captureSignal(true, false)
			requireIncident(t, db, schema, c, rule, AlertStateOpen, 1)
			wall.advance(time.Minute)
			captureSignal(false, true)
			requireIncident(t, db, schema, c, rule, AlertStateOpen, 1)
			captureSignal(false, false)
			row := requireIncident(t, db, schema, c, rule, AlertStateRecovered, 1)
			if row.RecoveredAt.IsNull() {
				t.Fatal("recovered incident lacks a recovery time")
			}
		})
	}
}

func TestPostgresTelemetryHTTPIncidentsExcludeMonitoringAndPreserveLowTraffic(t *testing.T) {
	for _, test := range []struct {
		name                     string
		business, businessStatus int
		want                     bool
	}{
		{"business-errors", 2, 500, true},
		{"healthy-business", 2, 200, false},
		{"insufficient-business", 1, 500, false},
		{"monitoring-only", 0, 500, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			cc := collectorConfig()
			cc.Routes.ExcludedPrefixes = []string{"monitor."}
			config := incidentConfig(RuleLatency, RuleHTTPErrors)
			config.Alerts.SustainCaptures = 1
			s, db, schema, wall := postgresStorage(t, cc, config)
			duration := time.Millisecond
			if test.businessStatus == 500 {
				duration = 2 * time.Second
			}
			observeIncidentTraffic(t, s.collector, "business.read", test.business, test.businessStatus, duration)
			observeIncidentTraffic(t, s.collector, "monitor.overview", 100, 500, 2*time.Second)
			capture(t, s.collector)
			if err := s.flush(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			for _, rule := range config.Alerts.Rules {
				if storedIncident(t, db, schema, s.collector, rule).IsSet() != test.want {
					t.Fatalf("monitoring changed %s decision", rule)
				}
			}
			if countTelemetry(t, db, schema).metrics < 2 {
				t.Fatal("excluded monitoring traffic was not retained")
			}
			if !test.want {
				return
			}
			wall.advance(time.Minute)
			for range 3 {
				capture(t, s.collector)
				if err := s.flush(t.Context(), nil); err != nil {
					t.Fatal(err)
				}
			}
			for _, rule := range config.Alerts.Rules {
				requireIncident(t, db, schema, s.collector, rule, AlertStateOpen, 1)
			}
			for _, p := range s.progress.Rules {
				if p.Bad != 0 || p.Healthy != 0 {
					t.Fatal("insufficient traffic retained progress")
				}
			}
		})
	}
}

func TestPostgresTelemetryLogCounterResetDoesNotBroadenTheRule(t *testing.T) {
	config := incidentConfig(RuleLogFailures)
	config.Alerts.SustainCaptures, config.Alerts.RecoverCaptures = 1, 1
	s, db, schema, _ := postgresStorage(t, collectorConfig(), config)
	for _, step := range []struct {
		writes, dropped float64
		state           AlertState
		episode         Episode
	}{{5, 0, AlertStateOpen, 1}, {0, 100, AlertStateRecovered, 1}, {0, 200, AlertStateRecovered, 1}, {1, 200, AlertStateOpen, 2}} {
		captureIncident(t, s.collector, 0, step.writes, step.dropped)
		if err := s.flush(t.Context(), nil); err != nil {
			t.Fatal(err)
		}
		requireIncident(t, db, schema, s.collector, RuleLogFailures, step.state, step.episode)
	}
}

func TestPostgresTelemetryIncidentProgressCommitsOnceAcrossUnknownOutcomes(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(fmt.Sprintf("rollback-%v", rollback), func(t *testing.T) {
			s, db, schema, _ := postgresStorage(t, collectorConfig(), incidentConfig(RulePoolPressure))
			faultDB, armed := commitAckDatabase(t, schema, rollback)
			s.transact = faultDB.Primary().Transaction
			first := captureIncident(t, s.collector, .95, 0, 0)
			armed.Store(true)
			if err := s.flush(t.Context(), nil); !errors.Is(err, database.CommitUnknown) {
				t.Fatalf("ambiguous incident flush = %v", err)
			}
			if len(s.progress.Rules) != 0 || len(s.collector.pending) != 1 || s.collector.pending[0].id != first.id {
				t.Fatal("ambiguous completion published progress or lost the batch")
			}
			if err := s.flush(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			if len(s.progress.Rules) != 1 || s.progress.Rules[0].Bad != 1 || storedIncident(t, db, schema, s.collector, RulePoolPressure).IsSet() {
				t.Fatal("receipt reconciliation counted one capture twice")
			}
			captureIncident(t, s.collector, .95, 0, 0)
			if err := s.flush(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			requireIncident(t, db, schema, s.collector, RulePoolPressure, AlertStateOpen, 1)
			if countTelemetry(t, db, schema).receipts != 2 {
				t.Fatal("reconciled capture wrote another receipt")
			}
		})
	}
}

func TestPostgresTelemetryIncidentAndReceiptRollbackTogether(t *testing.T) {
	config := incidentConfig(RulePoolPressure)
	config.Alerts.SustainCaptures = 1
	s, db, schema, _ := postgresStorage(t, collectorConfig(), config)
	abort := errors.New("abort incident transaction")
	writer := s.transact
	s.transact = func(ctx context.Context, fn func(*database.Tx) error, options ...database.TxOptions) error {
		return writer(ctx, func(tx *database.Tx) error {
			if err := fn(tx); err != nil {
				return err
			}
			return abort
		}, options...)
	}
	captureIncident(t, s.collector, .95, 0, 0)
	if err := s.flush(t.Context(), nil); !errors.Is(err, abort) {
		t.Fatal("expected abort", err)
	}
	if countTelemetry(t, db, schema) != (telemetryCounts{}) || storedIncident(t, db, schema, s.collector, RulePoolPressure).IsSet() || len(s.progress.Rules) != 0 {
		t.Fatal("rolled-back incident or receipt/progress survived")
	}
	s.transact = writer
	if err := s.flush(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	requireIncident(t, db, schema, s.collector, RulePoolPressure, AlertStateOpen, 1)
}

func TestPostgresTelemetryIncidentEpisodeOverflowAndBackwardsTime(t *testing.T) {
	config := incidentConfig(RulePoolPressure)
	config.Alerts.SustainCaptures, config.Alerts.RecoverCaptures = 1, 1
	s, db, schema, wall := postgresStorage(t, collectorConfig(), config)
	for _, pool := range []float64{.95, 0} {
		captureIncident(t, s.collector, pool, 0, 0)
		if err := s.flush(t.Context(), nil); err != nil {
			t.Fatal(err)
		}
		wall.advance(time.Second)
	}
	prior := requireIncident(t, db, schema, s.collector, RulePoolPressure, AlertStateRecovered, 1)
	wall.advance(-time.Minute)
	captureIncident(t, s.collector, .95, 0, 0)
	if err := s.flush(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	reopened := requireIncident(t, db, schema, s.collector, RulePoolPressure, AlertStateOpen, 2)
	if reopened.LastAt.UTC().Before(prior.LastAt.UTC()) || reopened.FirstAt != reopened.LastAt || !reopened.RecoveredAt.IsNull() {
		t.Fatal("backwards capture time regressed the incident")
	}
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		_, err := orm.Update(t.Context(), tx, alerts.Query(), alerts.Key(), reopened.Key, orm.EnumSet(alerts.State, AlertStateRecovered), orm.NumberSet(alerts.Episode, Episode(math.MaxInt64)))
		return err
	})
	captureIncident(t, s.collector, .95, 0, 0)
	if err := s.flush(t.Context(), nil); !errors.Is(err, fault.Conflict) {
		t.Fatalf("exhausted episode = %v", err)
	}
	requireIncident(t, db, schema, s.collector, RulePoolPressure, AlertStateRecovered, Episode(math.MaxInt64))
	if countTelemetry(t, db, schema).receipts != 3 || len(s.collector.pending) != 1 {
		t.Fatal("episode overflow committed a receipt or discarded its capture")
	}
}

type incidentActor struct {
	Key     string
	Allowed bool
}

type incidentAttribution struct {
	Key     AlertKey
	Episode Episode
	Actor   string
}

type incidentActorTable struct {
	orm.Table[incidentActor, string]
	Allowed orm.Column[incidentActor, bool]
}

type incidentAttributionTable struct {
	orm.Table[incidentAttribution, AlertKey]
	Episode orm.Number[incidentAttribution, Episode]
	Actor   orm.Text[incidentAttribution, string]
}

// errActorRevoked stands in for an application's own authorization failure;
// the framework never authorizes acknowledgements.
var errActorRevoked = errors.New("actor permission revoked")

var incidentActors = orm.Define("telemetry_test_actors", func(b orm.Builder[incidentActor], m *incidentActor) incidentActorTable {
	return incidentActorTable{Table: orm.TableOf(b, &m.Key), Allowed: orm.BoolOf(b, &m.Allowed)}
}, orm.NoTimestamps())

var incidentAttributions = orm.Define("telemetry_test_attributions", func(b orm.Builder[incidentAttribution], m *incidentAttribution) incidentAttributionTable {
	return incidentAttributionTable{Table: orm.TableOf(b, &m.Key), Episode: orm.NumberOf(b, &m.Episode), Actor: orm.TextOf(b, &m.Actor)}
}, orm.NoTimestamps())

func TestPostgresTelemetryAcknowledgementSharesActorTransactionAndEpisodes(t *testing.T) {
	config := incidentConfig(RulePoolPressure)
	config.Alerts.SustainCaptures, config.Alerts.RecoverCaptures = 1, 1
	s, db, schema, _ := postgresStorage(t, collectorConfig(), config)
	e := &Engine{storage: s, collector: s.collector, deps: Dependencies{DB: db, Schema: schema}}
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		if _, err := tx.Exec(t.Context(), `CREATE TABLE telemetry_test_actors (key text PRIMARY KEY, allowed boolean NOT NULL)`); err != nil {
			return err
		}
		if _, err := tx.Exec(t.Context(), `CREATE TABLE telemetry_test_attributions (key text PRIMARY KEY, episode bigint NOT NULL, actor text NOT NULL REFERENCES telemetry_test_actors(key))`); err != nil {
			return err
		}
		_, err := orm.Insert(t.Context(), tx, incidentActors.Table, orm.KeySet(incidentActors.Key(), "developer"), orm.ColumnSet(incidentActors.Allowed, true))
		return err
	})
	captureIncident(t, s.collector, .95, 0, 0)
	if err := s.flush(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	opened := requireIncident(t, db, schema, s.collector, RulePoolPressure, AlertStateOpen, 1)
	abort := errors.New("abort app acknowledgement")
	acknowledge := func(rollback bool) (Acknowledgement, error) {
		var result Acknowledgement
		err := db.Transaction(t.Context(), func(tx *database.Tx) error {
			if _, err := tx.Exec(t.Context(), `SET LOCAL search_path TO "`+schema+`", pg_catalog`); err != nil {
				return err
			}
			actor, err := orm.RequireLocked(t.Context(), tx, orm.Lock(incidentActors.Query()).Where(orm.KeyEq(incidentActors.Key(), "developer")))
			if err != nil {
				return err
			}
			if !actor.Allowed {
				return errActorRevoked
			}
			var before, after string
			if err := database.ScanOne(t.Context(), tx, `SELECT pg_catalog.current_setting('search_path')`, nil, &before); err != nil {
				return err
			}
			result, err = e.AcknowledgeIn(t.Context(), tx, opened.Key)
			if err != nil {
				return err
			}
			if err := database.ScanOne(t.Context(), tx, `SELECT pg_catalog.current_setting('search_path')`, nil, &after); err != nil {
				return err
			}
			if before != after {
				return errors.New("acknowledgement changed caller schema scope")
			}
			conflict := orm.OnConflict(orm.ConflictKey(orm.KeyExpr(incidentAttributions.Key()))).DoUpdate(orm.ConflictIncoming(orm.NumberExpr(incidentAttributions.Episode)), orm.ConflictIncoming(orm.TextExpr(incidentAttributions.Actor)))
			if _, err := orm.Upsert(t.Context(), tx, incidentAttributions.Query(), conflict, orm.KeySet(incidentAttributions.Key(), result.Key), orm.NumberSet(incidentAttributions.Episode, result.Episode), orm.TextSet(incidentAttributions.Actor, actor.Key)); err != nil {
				return err
			}
			if rollback {
				return abort
			}
			return nil
		})
		return result, err
	}
	if _, err := acknowledge(true); !errors.Is(err, abort) {
		t.Fatal("expected outer rollback", err)
	}
	requireIncident(t, db, schema, s.collector, RulePoolPressure, AlertStateOpen, 1)
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		count, err := orm.Count(t.Context(), tx, incidentAttributions.Query())
		if err == nil && count != 0 {
			return errors.New("actor attribution escaped outer rollback")
		}
		return err
	})
	for range 2 {
		ack, err := acknowledge(false)
		if err != nil || ack != (Acknowledgement{Key: opened.Key, Episode: 1}) {
			t.Fatalf("acknowledgement = %+v, %v", ack, err)
		}
	}
	requireIncident(t, db, schema, s.collector, RulePoolPressure, AlertStateAcknowledged, 1)
	captureIncident(t, s.collector, 0, 0, 0)
	if err := s.flush(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	requireIncident(t, db, schema, s.collector, RulePoolPressure, AlertStateRecovered, 1)
	if _, err := acknowledge(false); !errors.Is(err, fault.Conflict) {
		t.Fatalf("recovered acknowledgement = %v", err)
	}
	captureIncident(t, s.collector, .95, 0, 0)
	if err := s.flush(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	reopened := requireIncident(t, db, schema, s.collector, RulePoolPressure, AlertStateOpen, 2)
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		attribution, err := orm.RequireKey(t.Context(), tx, incidentAttributions.Table, opened.Key)
		if err != nil {
			return err
		}
		if attribution.Episode == reopened.Episode || attribution.Episode != 1 {
			return errors.New("reopen did not invalidate the prior actor attribution")
		}
		_, err = orm.Update(t.Context(), tx, incidentActors.Query(), incidentActors.Key(), "developer", orm.ColumnSet(incidentActors.Allowed, false))
		return err
	})
	if _, err := acknowledge(false); !errors.Is(err, errActorRevoked) {
		t.Fatalf("revoked actor acknowledgement = %v", err)
	}
	requireIncident(t, db, schema, s.collector, RulePoolPressure, AlertStateOpen, 2)
	storageTransaction(t, db, schema, func(tx *database.Tx) error {
		_, err := orm.Update(t.Context(), tx, incidentActors.Query(), incidentActors.Key(), "developer", orm.ColumnSet(incidentActors.Allowed, true))
		return err
	})
	ack, err := acknowledge(false)
	if err != nil || ack.Episode != 2 {
		t.Fatalf("new-episode acknowledgement = %+v, %v", ack, err)
	}
	requireIncident(t, db, schema, s.collector, RulePoolPressure, AlertStateAcknowledged, 2)
}

func TestPostgresTelemetryAcknowledgementRejectsForeignTransactionsAndClosedOwner(t *testing.T) {
	s, db, schema, _ := postgresStorage(t, collectorConfig(), incidentConfig(RulePoolPressure))
	e := &Engine{storage: s, collector: s.collector, deps: Dependencies{DB: db, Schema: schema}}
	key := AlertKey(digest("known-shape"))
	if _, err := e.AcknowledgeIn(t.Context(), nil, key); !errors.Is(err, fault.Invalid) {
		t.Fatalf("nil transaction = %v", err)
	}
	foreign, _ := commitAckDatabase(t, schema, false)
	if err := foreign.Transaction(t.Context(), func(tx *database.Tx) error {
		_, err := e.AcknowledgeIn(t.Context(), tx, key)
		if !errors.Is(err, fault.Invalid) {
			return fmt.Errorf("foreign transaction accepted: %w", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	e.operations.seal()
	if err := db.Transaction(t.Context(), func(tx *database.Tx) error {
		_, err := e.AcknowledgeIn(t.Context(), tx, key)
		if !errors.Is(err, fault.Closed) {
			return fmt.Errorf("closed owner accepted acknowledgement: %w", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresTelemetryReopenReceiptNeverAdvancesEpisodeTwice(t *testing.T) {
	config := incidentConfig(RulePoolPressure)
	config.Alerts.SustainCaptures, config.Alerts.RecoverCaptures = 1, 1
	s, db, schema, _ := postgresStorage(t, collectorConfig(), config)
	for _, pool := range []float64{.95, 0} {
		captureIncident(t, s.collector, pool, 0, 0)
		if err := s.flush(t.Context(), nil); err != nil {
			t.Fatal(err)
		}
	}
	requireIncident(t, db, schema, s.collector, RulePoolPressure, AlertStateRecovered, 1)
	faultDB, armed := commitAckDatabase(t, schema, false)
	s.transact = faultDB.Primary().Transaction
	captureIncident(t, s.collector, .95, 0, 0)
	armed.Store(true)
	if err := s.flush(t.Context(), nil); !errors.Is(err, database.CommitUnknown) {
		t.Fatalf("unknown reopen = %v", err)
	}
	requireIncident(t, db, schema, s.collector, RulePoolPressure, AlertStateOpen, 2)
	if s.progress.Rules[0].Healthy != 1 {
		t.Fatal("unknown reopen published its progress")
	}
	if err := s.flush(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	requireIncident(t, db, schema, s.collector, RulePoolPressure, AlertStateOpen, 2)
	if s.progress.Rules[0].Bad != 1 || countTelemetry(t, db, schema).receipts != 3 {
		t.Fatal("reopen receipt was not reconciled exactly once")
	}
}

func TestPostgresTelemetryIncidentPublishesDespiteCommittedCompletionError(t *testing.T) {
	s, db, schema, _ := postgresStorage(t, collectorConfig(), incidentConfig(RulePoolPressure))
	completion := errors.New("post-commit fixture failure")
	writer := s.transact
	s.transact = func(ctx context.Context, fn func(*database.Tx) error, options ...database.TxOptions) error {
		return writer(ctx, func(tx *database.Tx) error {
			if err := fn(tx); err != nil {
				return err
			}
			return tx.AfterCommit(func(context.Context) error { return completion })
		}, options...)
	}
	captureIncident(t, s.collector, .95, 0, 0)
	if err := s.flush(t.Context(), nil); !errors.Is(err, database.AfterCommitFailed) || !errors.Is(err, completion) {
		t.Fatalf("completion error = %v", err)
	}
	if len(s.collector.pending) != 0 || len(s.progress.Rules) != 1 || s.progress.Rules[0].Bad != 1 {
		t.Fatal("confirmed commit did not publish incident progress")
	}
	s.transact = writer
	if err := s.flush(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if countTelemetry(t, db, schema).receipts != 1 || storedIncident(t, db, schema, s.collector, RulePoolPressure).IsSet() {
		t.Fatal("empty flush re-evaluated an already committed capture")
	}
	captureIncident(t, s.collector, .95, 0, 0)
	if err := s.flush(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	requireIncident(t, db, schema, s.collector, RulePoolPressure, AlertStateOpen, 1)
}
