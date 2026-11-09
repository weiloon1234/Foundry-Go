package redis

import (
	"maps"
	"strconv"
	"testing"
	"time"

	driver "github.com/redis/go-redis/v9"
)

func jobKeyExpirations(t *testing.T, f quarantineFixture) map[string]int64 {
	t.Helper()
	result := make(map[string]int64, len(f.keys))
	for _, key := range f.keys {
		expiry, err := f.client.raw.Do(t.Context(), "PEXPIRETIME", key).Int64()
		if err != nil {
			t.Fatal(err)
		}
		result[key] = expiry
	}
	return result
}

func TestRedisInspectionAndMigrationValidationMatrix(t *testing.T) {
	for _, layout := range []JobQueueLayout{JobQueueLayoutAbsent, JobQueueLayout2, JobQueueLayout3} {
		type validationCase struct {
			name    string
			damage  func(*testing.T, quarantineFixture)
			blocker JobLayoutBlocker
		}
		cases := []validationCase{
			{name: "valid"},
			{name: "orphan_or_missing_policy", damage: func(t *testing.T, f quarantineFixture) {
				if layout == JobQueueLayoutAbsent {
					if err := f.client.raw.HSet(t.Context(), f.keys[0], "orphan", "opaque").Err(); err != nil {
						t.Fatal(err)
					}
				} else if err := f.client.raw.HDel(t.Context(), f.keys[4], "config").Err(); err != nil {
					t.Fatal(err)
				}
			}, blocker: JobLayoutBlockerInvalidState},
		}
		if layout != JobQueueLayoutAbsent {
			cases = append(cases, validationCase{
				name: "policy", damage: func(t *testing.T, f quarantineFixture) {
					if err := f.client.raw.HSet(t.Context(), f.keys[4], "config", "different").Err(); err != nil {
						t.Fatal(err)
					}
				}, blocker: JobLayoutBlockerPolicyMismatch,
			})
			for _, field := range []string{"layout", "time", "live", "live_bytes", "retained", "retained_bytes", "failed"} {
				cases = append(cases, validationCase{
					name: "invalid_" + field, damage: func(t *testing.T, f quarantineFixture) {
						if err := f.client.raw.HSet(t.Context(), f.keys[4], field, "-1").Err(); err != nil {
							t.Fatal(err)
						}
					}, blocker: JobLayoutBlockerInvalidState,
				})
				if field == "layout" {
					cases[len(cases)-1].blocker = JobLayoutBlockerUnsupportedLayout
				}
			}
			cases = append(cases, validationCase{
				name: "quarantine", damage: func(t *testing.T, f quarantineFixture) {
					if err := f.client.raw.ZAdd(t.Context(), f.keys[10], driver.Z{Score: 1, Member: "opaque-marker"}).Err(); err != nil {
						t.Fatal(err)
					}
				},
			})
			if layout == JobQueueLayout2 {
				cases[len(cases)-1].blocker = JobLayoutBlockerInvalidState
			}
		}
		for index := range 11 {
			cases = append(cases, validationCase{
				name: "wrong_type_" + strconv.Itoa(index), damage: func(t *testing.T, f quarantineFixture) {
					if err := f.client.raw.Set(t.Context(), f.keys[index], "wrong-type", 0).Err(); err != nil {
						t.Fatal(err)
					}
				}, blocker: JobLayoutBlockerInvalidState,
			})
		}
		for _, tc := range cases {
			t.Run(string(layout)+"/"+tc.name, func(t *testing.T) {
				f := newQuarantineFixture(t)
				switch layout {
				case JobQueueLayout2:
					oldJobEnqueue(t, f, f.pending(t).Envelope())
				case JobQueueLayout3:
					f.enqueue(t)
				}
				if tc.damage != nil {
					tc.damage(t, f)
				}
				// Absolute expiry remains stable across read-only observations, unlike PTTL.
				expires := time.Now().Add(time.Hour)
				for _, key := range f.keys {
					if err := f.client.raw.PExpireAt(t.Context(), key, expires).Err(); err != nil {
						t.Fatal(err)
					}
				}
				before, expirations := snapshotJobKeys(t, f.client, f.keys), jobKeyExpirations(t, f)
				inspection, err := f.backend.InspectLayout(t.Context(), f.key)
				if err != nil {
					t.Fatal(err)
				}
				eligible := tc.blocker == JobLayoutBlockerNone
				if inspection.Eligible != eligible || inspection.Blocker != tc.blocker {
					t.Fatalf("inspection = %+v, want eligible=%v blocker=%s", inspection, eligible, tc.blocker)
				}
				if eligible && inspection.Layout != layout {
					t.Fatalf("layout = %s, want %s", inspection.Layout, layout)
				}
				if !maps.Equal(before, snapshotJobKeys(t, f.client, f.keys)) || !maps.Equal(expirations, jobKeyExpirations(t, f)) {
					t.Fatal("inspection changed bytes, types, or expiration metadata")
				}
				changed, migrationErr := f.backend.JobMigrateLayout(t.Context(), f.key)
				if (migrationErr == nil) != eligible {
					t.Fatalf("inspection/migration disagree: %+v, migration=%v", inspection, migrationErr)
				}
				if !eligible {
					if changed || !maps.Equal(before, snapshotJobKeys(t, f.client, f.keys)) || !maps.Equal(expirations, jobKeyExpirations(t, f)) {
						t.Fatal("rejected migration changed state")
					}
					return
				}
				if changed != (layout == JobQueueLayout2) {
					t.Fatalf("changed = %v for %s", changed, layout)
				}
				after, err := f.backend.InspectLayout(t.Context(), f.key)
				if err != nil || !after.Eligible || after.Layout != JobQueueLayout3 {
					t.Fatalf("post-migration inspection = %+v, %v", after, err)
				}
			})
		}
	}
}
