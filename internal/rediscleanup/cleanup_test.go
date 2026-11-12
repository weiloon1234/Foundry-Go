package rediscleanup

import (
	"context"
	"fmt"
	"slices"
	"testing"

	driver "github.com/redis/go-redis/v9"
	"github.com/weiloon1234/Foundry-Go/keyspace"
)

type scanPage struct {
	keys []string
	next uint64
}
type cleanupClient struct {
	t        *testing.T
	prefix   string
	pages    []scanPage
	cursors  []uint64
	unlinked []string
}

func (c *cleanupClient) Scan(ctx context.Context, cursor uint64, match string, count int64) *driver.ScanCmd {
	c.t.Helper()
	if match != c.prefix+"*" || count != batchSize {
		c.t.Fatalf("unsafe scan: %q COUNT %d", match, count)
	}
	c.cursors = append(c.cursors, cursor)
	if len(c.pages) == 0 {
		c.t.Fatal("unexpected extra scan")
	}
	page := c.pages[0]
	c.pages = c.pages[1:]
	cmd := driver.NewScanCmd(ctx, nil)
	cmd.SetVal(page.keys, page.next)
	return cmd
}
func (c *cleanupClient) Unlink(ctx context.Context, keys ...string) *driver.IntCmd {
	if len(keys) == 0 || len(keys) > batchSize {
		c.t.Fatalf("unbounded unlink batch: %d", len(keys))
	}
	c.unlinked = append(c.unlinked, keys...)
	return driver.NewIntResult(int64(len(keys)), nil)
}
func TestCleanupScansExactPrefixAndBoundsUnlink(t *testing.T) {
	prefixes, err := Prefixes(keyspace.Namespace{Application: "owned", Environment: "test"}, "jobs")
	if err != nil {
		t.Fatal(err)
	}
	prefix := prefixes[0]
	var keys []string
	for i := range batchSize*2 + 3 {
		keys = append(keys, fmt.Sprintf("%s%d", prefix, i))
	}
	client := &cleanupClient{t: t, prefix: prefix, pages: []scanPage{{next: 7}, {keys: keys, next: 11}, {}}}
	if err := remove(t.Context(), client, prefix); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(client.cursors, []uint64{0, 7, 11}) || !slices.Equal(client.unlinked, keys) {
		t.Fatal("scan cursor or owned keys lost")
	}
}
func TestCleanupRefusesForeignScanResults(t *testing.T) {
	client := &cleanupClient{t: t, prefix: "foundry:jobs:v1:owned:test:", pages: []scanPage{{keys: []string{"foundry:jobs:v1:other:test:key"}}}}
	if err := remove(t.Context(), client, client.prefix); err == nil || len(client.unlinked) != 0 {
		t.Fatal("foreign key was eligible for deletion")
	}
}
func TestCleanupRejectsGlobComponents(t *testing.T) {
	for _, input := range []struct {
		namespace keyspace.Namespace
		feature   keyspace.Feature
	}{
		{keyspace.Namespace{Application: "*", Environment: "test"}, "jobs"},
		{keyspace.Namespace{Application: "owned", Environment: "?"}, "jobs"},
		{keyspace.Namespace{Application: "owned", Environment: "test"}, "[jobs]"},
	} {
		if _, err := Prefixes(input.namespace, input.feature); err == nil {
			t.Fatal("glob component accepted")
		}
	}
}

type retainedCleanup struct {
	testing.TB
	cleanup  func()
	prefixes []string
}

func (t *retainedCleanup) Failed() bool      { return true }
func (t *retainedCleanup) Cleanup(fn func()) { t.cleanup = fn }
func (t *retainedCleanup) Logf(_ string, args ...any) {
	t.prefixes = append(t.prefixes, args[0].(string))
}

func TestTrackerRetainsExactAdditionalNamespacesWithoutRedis(t *testing.T) {
	probe := &retainedCleanup{TB: t}
	initial := []string{"foundry:cache:v1:owned:test:"}
	track := Register(probe, func() *driver.Client { t.Fatal("retained cleanup opened a client"); return nil }, initial)
	initial[0] = "changed"
	track("foundry:cache-namespace:v1:owned:test:generation")
	track("foundry:raw:v1:owned:test-other:values:key")
	track("foundry:raw:v1:owned:test-other:values:another")
	probe.cleanup()
	want := []string{"foundry:cache-namespace:v1:owned:test:", "foundry:cache:v1:owned:test:", "foundry:raw:v1:owned:test-other:"}
	if !slices.Equal(probe.prefixes, want) {
		t.Fatalf("retained prefixes = %v, want %v", probe.prefixes, want)
	}
}
