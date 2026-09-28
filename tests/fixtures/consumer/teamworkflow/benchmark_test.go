package teamworkflow

import (
	"crypto/rand"
	"fmt"
	"github.com/weiloon1234/Foundry-Go/application"
	pgtest "github.com/weiloon1234/Foundry-Go/testkit/postgres"
	"testing"
)

// Includes the native HTTP server/client, typed nested policy, PostgreSQL claim,
// domain writes, response persistence, and managed outbox/receiver activity.
func BenchmarkWorkflowHTTP(b *testing.B) {
	app := startApp(b, pgtest.Isolate(b), pgtest.Isolate(b), Hooks{}, func(s *application.Settings) { s.Features.Idempotency.Config.MaxRetainedPerCaller = 100000 })
	db, _ := app.app.Resources().Database()
	for _, mode := range []string{"New", "Replay"} {
		b.Run(mode, func(b *testing.B) {
			prefix := "workflow-cost-" + rand.Text() + "-"
			key := prefix + "warm"
			path, body := "/teams/1/projects/shared/submissions", `{"name":"Benchmark"}`
			warm := send(b, app, "POST", path, "alice", key, body, 202)
			before := scalar(b, db, `SELECT count(*) FROM workflow_submissions`)
			count := 0
			b.ReportAllocs()
			for b.Loop() {
				count++
				if mode == "New" {
					key = fmt.Sprintf("%s%d", prefix, count)
				}
				response, err := app.send(b.Context(), "POST", path, "alice", key, "application/json", []byte(body))
				if err != nil || response.Status() != 202 {
					b.Fatal("workflow benchmark request failed", err)
				}
			}
			delta := scalar(b, db, `SELECT count(*) FROM workflow_submissions`) - before
			want := int64(0)
			if mode == "New" {
				want = int64(count)
			}
			if delta != want {
				b.Fatal("benchmark did not measure its declared new/replay mode")
			}
			b.ReportMetric(float64(len(warm.Bytes())), "response-B/op")
		})
	}
}
