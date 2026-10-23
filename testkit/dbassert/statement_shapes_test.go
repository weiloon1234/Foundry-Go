package dbassert

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/weiloon1234/Foundry-Go/database"
)

type shapeTest struct{ errors []string }

func (t *shapeTest) Helper() {}
func (t *shapeTest) Errorf(s string, args ...any) {
	t.errors = append(t.errors, fmt.Sprintf(s, args...))
}
func TestStatementShapes(t *testing.T) {
	d, _ := database.NewStatementDetector(database.StatementDetectorConfig{Threshold: 1, MaxShapes: 1})
	ctx, s := d.Begin(context.Background())
	fake := &shapeTest{}
	assertStatementShapes(fake, s)
	if len(fake.errors) != 0 {
		t.Fatal(fake.errors)
	}
	for range 2 {
		d.Observe(ctx, database.QueryEvent{Statement: "select 'canary'"})
	}
	d.Observe(ctx, database.QueryEvent{Statement: "select id from users"})
	d.Observe(ctx, database.QueryEvent{Statement: "select 'canary"})
	assertStatementShapes(fake, s)
	if len(fake.errors) != 2 || strings.Contains(strings.Join(fake.errors, " "), "canary") {
		t.Fatal(fake.errors)
	}
}

func TestStatementSettingValuesRedacted(t *testing.T) {
	for _, value := range []string{"canary_token", `"canary_token"`, "'canary_token'"} {
		for _, sql := range []string{"SET app.token TO " + value, "SELECT set_config('token'," + value + ",false)", "SELECT id,current_setting(" + value + ") FROM records", "SELECT id,set_config('token'," + value + ",false) FROM records"} {
			d, _ := database.NewStatementDetector(database.StatementDetectorConfig{Threshold: 1, MaxShapes: 8})
			ctx, scope := d.Begin(t.Context())
			for range 2 {
				d.Observe(ctx, database.QueryEvent{Statement: sql})
			}
			reporter := &shapeTest{}
			assertStatementShapes(reporter, scope)
			if len(reporter.errors) != 1 || strings.Contains(strings.Join(reporter.errors, " "), "canary") {
				t.Fatal(reporter.errors)
			}
		}
	}
}
